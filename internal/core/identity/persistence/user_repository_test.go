package persistence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const userTestTimeout = 5 * time.Second

const (
	seedPasswordDigest    = "$2a$10$seeddigestseeddigestseeddigestseeddigestseeddigestsee"
	rotatedPasswordDigest = "$2a$10$rotateddigestrotateddigestrotateddigestrotateddigestro"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	setDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setDefaultEnv(t, "POSTGRES_DB", "tuenti")

	setDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:8080/password/reset")
	setDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:8080/confirm-email")
	setDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:8080/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), userTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	return pgConn.Pool()
}

func setDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func createTestUser(t *testing.T, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), userTestTimeout)
	defer cancel()

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:           "Wile E. Coyote " + suffix,
		Email:          "wile." + suffix + "@example.com",
		PasswordDigest: seedPasswordDigest,
	}
	require.NoError(t, NewUserRepository(pool).Create(ctx, user))

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), userTestTimeout)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for user %v: %v", user.Id, err)
		}
	})

	return user
}

func randomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func userTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), userTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func unknownUserID(t *testing.T) pgtype.UUID {
	t.Helper()

	id := pgtype.UUID{Valid: true}
	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	return id
}

func confirmedAtOf(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) *time.Time {
	t.Helper()

	var confirmedAt *time.Time

	require.NoError(t, pool.QueryRow(
		userTestContext(t), `SELECT confirmed_at FROM users WHERE id = $1`, id,
	).Scan(&confirmedAt))

	return confirmedAt
}

func TestUserRepositoryFindByID(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(pool)
	ctx := userTestContext(t)

	user := createTestUser(t, pool)

	found, err := repository.FindByID(ctx, user.Id)
	require.NoError(t, err)

	assert.Equal(t, user.Id, found.Id)
	assert.Equal(t, user.Name, found.Name)
	assert.Equal(t, user.Email, found.Email)
	assert.Equal(t, seedPasswordDigest, found.PasswordDigest)
	assert.False(t, found.CreatedAt.IsZero(), "the database must hand back created_at")

	t.Run("it agrees with FindByEmail", func(t *testing.T) {
		byEmail, err := repository.FindByEmail(ctx, user.Email)
		require.NoError(t, err)

		assert.Equal(t, byEmail.Id, found.Id, "both lookups must resolve to the same row")
		assert.Equal(t, byEmail.ConfirmedAt, found.ConfirmedAt)
	})
}

func TestUserRepositoryFindByIDForUnknownUser(t *testing.T) {
	pool := newTestPool(t)

	user, err := NewUserRepository(pool).FindByID(userTestContext(t), unknownUserID(t))

	assert.Nil(t, user)
	assert.ErrorIs(t, err, domain.ErrUserNotFound,
		"a missing row is a domain miss, not a leaked driver error")
}

func TestUserRepositoryFreshUserIsUnconfirmed(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(pool)
	ctx := userTestContext(t)

	user := createTestUser(t, pool)

	byEmail, err := repository.FindByEmail(ctx, user.Email)
	require.NoError(t, err)

	byID, err := repository.FindByID(ctx, user.Id)
	require.NoError(t, err)

	assert.Nil(t, byEmail.ConfirmedAt, "a freshly created user has not confirmed their email")
	assert.Nil(t, byID.ConfirmedAt, "a freshly created user has not confirmed their email")

	assert.False(t, byEmail.IsConfirmed(),
		"treating an unconfirmed user as confirmed would open every gate the confirmation check guards")
	assert.False(t, byID.IsConfirmed())
}

func TestUserRepositoryMarkConfirmed(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(pool)
	ctx := userTestContext(t)

	user := createTestUser(t, pool)

	original, err := repository.FindByID(ctx, user.Id)
	require.NoError(t, err)
	require.Nil(t, original.ConfirmedAt, "the fixture must start unconfirmed")

	confirmedAt := time.Now().UTC()
	require.NoError(t, repository.MarkConfirmed(ctx, user.Id, confirmedAt))

	require.NotNil(t, confirmedAtOf(t, pool, user.Id), "confirmed_at must reach the row itself")

	t.Run("it round-trips through FindByID", func(t *testing.T) {
		found, err := repository.FindByID(ctx, user.Id)
		require.NoError(t, err)
		require.NotNil(t, found.ConfirmedAt)

		assert.WithinDuration(t, confirmedAt, *found.ConfirmedAt, time.Millisecond)
		assert.True(t, found.IsConfirmed())
	})

	t.Run("it round-trips through FindByEmail", func(t *testing.T) {
		found, err := repository.FindByEmail(ctx, user.Email)
		require.NoError(t, err)
		require.NotNil(t, found.ConfirmedAt,
			"signin loads users by email, so a stale SELECT list here would hide the real state")

		assert.WithinDuration(t, confirmedAt, *found.ConfirmedAt, time.Millisecond)
		assert.True(t, found.IsConfirmed())
	})

	t.Run("the rest of the row is untouched", func(t *testing.T) {
		updated, err := repository.FindByID(ctx, user.Id)
		require.NoError(t, err)

		assert.Equal(t, original.Name, updated.Name)
		assert.Equal(t, original.Email, updated.Email)
		assert.Equal(t, original.PasswordDigest, updated.PasswordDigest,
			"confirming an email must never rotate the password digest")
		assert.Equal(t, original.CreatedAt, updated.CreatedAt, "confirming must not restamp created_at")
		assert.True(t, updated.UpdatedAt.After(original.UpdatedAt),
			"updated_at must advance: was %s, now %s", original.UpdatedAt, updated.UpdatedAt)
	})
}

func TestUserRepositoryMarkConfirmedTouchesOnlyTheTargetUser(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(pool)
	ctx := userTestContext(t)

	target := createTestUser(t, pool)
	bystander := createTestUser(t, pool)

	require.NoError(t, repository.MarkConfirmed(ctx, target.Id, time.Now().UTC()))

	untouched, err := repository.FindByID(ctx, bystander.Id)
	require.NoError(t, err)

	assert.Nil(t, untouched.ConfirmedAt,
		"confirming one user must never confirm another account")
}

func TestUserRepositoryMarkConfirmedForUnknownUser(t *testing.T) {
	pool := newTestPool(t)

	assert.NoError(t, NewUserRepository(pool).MarkConfirmed(userTestContext(t), unknownUserID(t), time.Now().UTC()),
		"an id that matches no row is a no-op, not a driver error")
}

func TestUserRepositoryUpdatePasswordDigest(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(pool)
	ctx := userTestContext(t)

	user := createTestUser(t, pool)

	original, err := repository.FindByEmail(ctx, user.Email)
	require.NoError(t, err)
	require.Equal(t, seedPasswordDigest, original.PasswordDigest, "the seed digest must be readable back")

	require.NoError(t, repository.UpdatePasswordDigest(ctx, user.Id, rotatedPasswordDigest))

	updated, err := repository.FindByEmail(ctx, user.Email)
	require.NoError(t, err)

	assert.Equal(t, rotatedPasswordDigest, updated.PasswordDigest, "the new digest must be persisted")
	assert.True(t, updated.UpdatedAt.After(original.UpdatedAt),
		"updated_at must advance: was %s, now %s", original.UpdatedAt, updated.UpdatedAt)

	t.Run("the rest of the row is untouched", func(t *testing.T) {
		assert.Equal(t, original.Id, updated.Id)
		assert.Equal(t, original.Name, updated.Name)
		assert.Equal(t, original.Email, updated.Email)
		assert.Equal(t, original.CreatedAt, updated.CreatedAt, "a password change must not restamp created_at")
	})
}

func TestUserRepositoryUpdatePasswordDigestTouchesOnlyTheTargetUser(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(pool)
	ctx := userTestContext(t)

	target := createTestUser(t, pool)
	bystander := createTestUser(t, pool)

	require.NoError(t, repository.UpdatePasswordDigest(ctx, target.Id, rotatedPasswordDigest))

	untouched, err := repository.FindByEmail(ctx, bystander.Email)
	require.NoError(t, err)

	assert.Equal(t, seedPasswordDigest, untouched.PasswordDigest,
		"rotating one user's password must never reach another account")
}

func TestUserRepositoryUpdatePasswordDigestForUnknownUser(t *testing.T) {
	pool := newTestPool(t)
	ctx := userTestContext(t)

	assert.NoError(t, NewUserRepository(pool).UpdatePasswordDigest(ctx, unknownUserID(t), rotatedPasswordDigest),
		"an id that matches no row is a no-op, not a driver error")
}
