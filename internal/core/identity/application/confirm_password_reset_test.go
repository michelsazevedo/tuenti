package application

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	confirmResetOldPassword = "the-password-they-forgot"
	confirmResetNewPassword = "the-password-they-just-chose"
)

const confirmResetSessionTTL = 5 * time.Minute

func newConfirmResetStore(t *testing.T) *persistence.RefreshTokenStore {
	t.Helper()

	addr := os.Getenv("REDIS_HOST")
	if addr == "" {
		addr = "localhost:6379"
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("redis unavailable at %s: %v (start it with `docker compose up -d redis`)", addr, err)
	}

	t.Cleanup(func() { _ = client.Close() })

	return persistence.NewRefreshTokenStore(client)
}

func createConfirmResetUser(t *testing.T, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	ctx := testContext(t)

	digest, err := bcrypt.GenerateFromPassword([]byte(confirmResetOldPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:           "Wile E. Coyote " + suffix,
		Email:          "wile." + suffix + "@example.com",
		PasswordDigest: string(digest),
	}
	require.NoError(t, persistence.NewUserRepository(pool).Create(ctx, user))

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for user %v: %v", user.Id, err)
		}
	})

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for the password reset tokens of user %v: %v", user.Id, err)
		}
	})

	return user
}

func seedConfirmResetToken(
	t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID, expiresAt time.Time,
) (string, *domain.PasswordResetToken) {
	t.Helper()

	rawToken, err := domain.GeneratePasswordResetToken()
	require.NoError(t, err)

	token := &domain.PasswordResetToken{
		UserID:      userID,
		TokenDigest: domain.HashPasswordResetToken(rawToken),
		ExpiresAt:   expiresAt,
	}
	require.NoError(t, persistence.NewPasswordResetTokenRepository(pool).Create(testContext(t), token))

	return rawToken, token
}

func plantConfirmResetSession(t *testing.T, store *persistence.RefreshTokenStore, userID string) string {
	t.Helper()

	rawRefresh, err := store.Save(testContext(t), userID, confirmResetSessionTTL)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		_ = store.RevokeAllForUser(cleanupCtx, userID)
	})

	return rawRefresh
}

func newTestConfirmPasswordReset(
	pgConn *database.PgConn, store *persistence.RefreshTokenStore,
) ConfirmPasswordResetUseCase {
	return NewConfirmPasswordReset(
		database.NewUnitOfWork(pgConn),
		persistence.NewPasswordResetTokenRepository(pgConn.Pool()),
		store,
	)
}

func passwordDigestOf(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()

	user, err := persistence.NewUserRepository(pool).FindByEmail(testContext(t), email)
	require.NoError(t, err)

	return user.PasswordDigest
}

func TestConfirmPasswordResetRotatesThePasswordBurnsTheTokenAndRevokesSessions(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	store := newConfirmResetStore(t)
	ctx := testContext(t)

	user := createConfirmResetUser(t, pool)
	rawToken, token := seedConfirmResetToken(t, pool, user.Id, time.Now().Add(passwordResetTokenTTL))

	// A session that predates the reset: it must not survive it.
	rawRefresh := plantConfirmResetSession(t, store, user.Id.String())
	_, err := store.Validate(ctx, rawRefresh)
	require.NoError(t, err, "the planted session must be live before the reset, or the assertion below proves nothing")

	require.NoError(t, newTestConfirmPasswordReset(pgConn, store).ConfirmPasswordReset(ctx, rawToken, confirmResetNewPassword))

	t.Run("the new password becomes the only one that works", func(t *testing.T) {
		digest := passwordDigestOf(t, pool, user.Email)

		assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(digest), []byte(confirmResetNewPassword)),
			"the chosen password must verify against the stored digest")
		assert.Error(t, bcrypt.CompareHashAndPassword([]byte(digest), []byte(confirmResetOldPassword)),
			"the forgotten password must stop working the moment the reset lands")
		assert.NotEqual(t, user.PasswordDigest, digest, "the stored digest must have been replaced")
	})

	t.Run("the token is burnt but still resolvable", func(t *testing.T) {
		burnt, err := persistence.NewPasswordResetTokenRepository(pool).
			FindByDigest(ctx, domain.HashPasswordResetToken(rawToken))
		require.NoError(t, err)

		require.NotNil(t, burnt.UsedAt, "used_at must be stamped by the reset")
		assert.Equal(t, token.Id, burnt.Id)
		assert.ErrorIs(t, burnt.Validate(time.Now()), domain.ErrPasswordResetTokenUsed)
	})

	t.Run("every session the old password could have opened is gone", func(t *testing.T) {
		_, err := store.Validate(ctx, rawRefresh)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid,
			"a session that predates the reset must no longer validate")

		_, _, err = store.Rotate(ctx, rawRefresh, confirmResetSessionTTL)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid,
			"a revoked session must not be able to rotate itself back to life")
	})
}

func TestConfirmPasswordResetRejectsAnUnknownToken(t *testing.T) {
	pgConn := newTestPgConn(t)
	store := newConfirmResetStore(t)
	ctx := testContext(t)

	err := newTestConfirmPasswordReset(pgConn, store).
		ConfirmPasswordReset(ctx, "not-a-token-anyone-ever-issued", confirmResetNewPassword)

	assert.ErrorIs(t, err, domain.ErrPasswordResetTokenInvalid,
		"a digest matching no row must surface unwrapped so the handler can branch on it")
}

func TestConfirmPasswordResetRejectsAnExpiredToken(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	store := newConfirmResetStore(t)
	ctx := testContext(t)

	user := createConfirmResetUser(t, pool)
	rawToken, _ := seedConfirmResetToken(t, pool, user.Id, time.Now().Add(-time.Minute))

	before := passwordDigestOf(t, pool, user.Email)

	err := newTestConfirmPasswordReset(pgConn, store).ConfirmPasswordReset(ctx, rawToken, confirmResetNewPassword)

	assert.ErrorIs(t, err, domain.ErrPasswordResetTokenExpired)
	assert.Equal(t, before, passwordDigestOf(t, pool, user.Email),
		"a token that expired must not be able to change the password")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(before), []byte(confirmResetOldPassword)),
		"the original password must still be the live one")
}

func TestConfirmPasswordResetRejectsAnAlreadyUsedToken(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	store := newConfirmResetStore(t)
	ctx := testContext(t)

	useCase := newTestConfirmPasswordReset(pgConn, store)

	user := createConfirmResetUser(t, pool)
	rawToken, _ := seedConfirmResetToken(t, pool, user.Id, time.Now().Add(passwordResetTokenTTL))

	require.NoError(t, useCase.ConfirmPasswordReset(ctx, rawToken, confirmResetNewPassword))
	afterFirst := passwordDigestOf(t, pool, user.Email)

	err := useCase.ConfirmPasswordReset(ctx, rawToken, "a-password-the-replay-tried-to-set")

	assert.ErrorIs(t, err, domain.ErrPasswordResetTokenUsed)
	assert.Equal(t, afterFirst, passwordDigestOf(t, pool, user.Email),
		"a replayed token must leave the password from the first reset untouched")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(afterFirst), []byte(confirmResetNewPassword)),
		"the password chosen in the first reset must still be the live one")
}
