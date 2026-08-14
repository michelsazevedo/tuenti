package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

const passwordResetTokenTTL = 30 * time.Minute

// createResetTestUser reuses the package's user fixture and adds the cleanup of
// whatever tokens the test hangs off it. Cleanups run LIFO, so registering this
// after createTestUser guarantees the children are gone before the parent row
// the foreign key points at.
func createResetTestUser(t *testing.T, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	user := createTestUser(t, pool)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), userTestTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for the password reset tokens of user %v: %v", user.Id, err)
		}
	})

	return user
}

// seedPasswordResetToken mints a real token and stores only its digest, exactly
// as the reset flow does. It returns the raw token so callers can assert it
// never reaches the database.
func seedPasswordResetToken(
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
	require.NoError(t, NewPasswordResetTokenRepository(pool).Create(userTestContext(t), token))

	return rawToken, token
}

// usedAtOf reads used_at straight from the row, bypassing the repository, so an
// assertion about what was persisted cannot be satisfied by in-memory state.
func usedAtOf(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) *time.Time {
	t.Helper()

	var usedAt *time.Time

	require.NoError(t, pool.QueryRow(
		userTestContext(t), `SELECT used_at FROM password_reset_tokens WHERE id = $1`, id,
	).Scan(&usedAt))

	return usedAt
}

func TestPasswordResetTokenRepositoryCreateAndFindByDigest(t *testing.T) {
	pool := newTestPool(t)
	repository := NewPasswordResetTokenRepository(pool)
	ctx := userTestContext(t)

	user := createResetTestUser(t, pool)
	expiresAt := time.Now().Add(passwordResetTokenTTL)

	rawToken, created := seedPasswordResetToken(t, pool, user.Id, expiresAt)

	require.True(t, created.Id.Valid, "the database must hand back the generated id")
	assert.False(t, created.CreatedAt.IsZero(), "the database must hand back created_at")
	assert.WithinDuration(t, time.Now(), created.CreatedAt, time.Minute)

	found, err := repository.FindByDigest(ctx, created.TokenDigest)
	require.NoError(t, err)

	assert.Equal(t, created.Id, found.Id)
	assert.Equal(t, user.Id, found.UserID)
	assert.Equal(t, created.TokenDigest, found.TokenDigest)
	assert.WithinDuration(t, expiresAt, found.ExpiresAt, time.Millisecond)
	assert.WithinDuration(t, created.CreatedAt, found.CreatedAt, time.Millisecond)
	assert.Nil(t, found.UsedAt, "a freshly minted token has not been used")

	t.Run("only the digest reaches the database", func(t *testing.T) {
		assert.NotEqual(t, rawToken, found.TokenDigest,
			"storing the raw token would let a database leak reset every account")
		assert.NotContains(t, found.TokenDigest, rawToken)

		token, err := repository.FindByDigest(ctx, rawToken)

		assert.Nil(t, token, "the raw token must not itself be a lookup key")
		assert.ErrorIs(t, err, domain.ErrPasswordResetTokenInvalid)
	})

	t.Run("the token is still redeemable", func(t *testing.T) {
		assert.NoError(t, found.Validate(time.Now()))
	})
}

func TestPasswordResetTokenRepositoryFindByDigestUnknown(t *testing.T) {
	pool := newTestPool(t)

	token, err := NewPasswordResetTokenRepository(pool).FindByDigest(
		userTestContext(t), domain.HashPasswordResetToken("never-issued-"+randomSuffix(t)),
	)

	assert.Nil(t, token)
	assert.ErrorIs(t, err, domain.ErrPasswordResetTokenInvalid,
		"an unknown digest is an invalid token, not a bare lookup miss")
}

func TestPasswordResetTokenRepositoryMarkUsed(t *testing.T) {
	pool := newTestPool(t)
	repository := NewPasswordResetTokenRepository(pool)
	ctx := userTestContext(t)

	user := createResetTestUser(t, pool)
	_, created := seedPasswordResetToken(t, pool, user.Id, time.Now().Add(passwordResetTokenTTL))

	require.NoError(t, repository.MarkUsed(ctx, created.Id))

	burned, err := repository.FindByDigest(ctx, created.TokenDigest)
	require.NoError(t, err)

	require.NotNil(t, burned.UsedAt, "MarkUsed must stamp used_at")
	assert.WithinDuration(t, time.Now(), *burned.UsedAt, time.Minute)
	assert.ErrorIs(t, burned.Validate(time.Now()), domain.ErrPasswordResetTokenUsed,
		"a burned token must no longer validate")

	t.Run("marking it again is a no-op", func(t *testing.T) {
		// The second call must land on a measurably later clock, so a missing
		// `used_at IS NULL` guard would show up as a moved timestamp.
		time.Sleep(50 * time.Millisecond)

		require.NoError(t, repository.MarkUsed(ctx, created.Id), "a retry must not error")

		again, err := repository.FindByDigest(ctx, created.TokenDigest)
		require.NoError(t, err)
		require.NotNil(t, again.UsedAt)

		assert.True(t, burned.UsedAt.Equal(*again.UsedAt),
			"used_at must keep reporting when the reset happened: was %s, now %s",
			burned.UsedAt, again.UsedAt)
	})
}

func TestPasswordResetTokenRepositoryMarkUsedForUnknownToken(t *testing.T) {
	pool := newTestPool(t)

	unknown := pgtype.UUID{Valid: true}
	copy(unknown.Bytes[:], []byte(randomSuffix(t)))

	assert.NoError(t, NewPasswordResetTokenRepository(pool).MarkUsed(userTestContext(t), unknown),
		"an id that matches no row is a no-op, not a driver error")
}

func TestPasswordResetTokenRepositoryInvalidateActiveForUser(t *testing.T) {
	pool := newTestPool(t)
	repository := NewPasswordResetTokenRepository(pool)
	ctx := userTestContext(t)

	target := createResetTestUser(t, pool)
	bystander := createResetTestUser(t, pool)

	_, firstActive := seedPasswordResetToken(t, pool, target.Id, time.Now().Add(passwordResetTokenTTL))
	_, secondActive := seedPasswordResetToken(t, pool, target.Id, time.Now().Add(passwordResetTokenTTL))
	_, alreadyUsed := seedPasswordResetToken(t, pool, target.Id, time.Now().Add(passwordResetTokenTTL))
	_, alreadyExpired := seedPasswordResetToken(t, pool, target.Id, time.Now().Add(-time.Hour))
	_, spared := seedPasswordResetToken(t, pool, bystander.Id, time.Now().Add(passwordResetTokenTTL))

	require.NoError(t, repository.MarkUsed(ctx, alreadyUsed.Id))

	burnedAt := usedAtOf(t, pool, alreadyUsed.Id)
	require.NotNil(t, burnedAt)

	// Any restamping must be visible against the instant already recorded.
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, repository.InvalidateActiveForUser(ctx, target.Id))

	for name, token := range map[string]*domain.PasswordResetToken{
		"first active":  firstActive,
		"second active": secondActive,
	} {
		found, err := repository.FindByDigest(ctx, token.TokenDigest)
		require.NoError(t, err)

		assert.NotNil(t, found.UsedAt, "the %s token outlived the new reset request", name)
		assert.ErrorIs(t, found.Validate(time.Now()), domain.ErrPasswordResetTokenUsed,
			"the %s token must no longer validate", name)
	}

	t.Run("an already-used token keeps its original instant", func(t *testing.T) {
		after := usedAtOf(t, pool, alreadyUsed.Id)
		require.NotNil(t, after)

		assert.True(t, burnedAt.Equal(*after),
			"restamping would misreport the reset as happening now: was %s, now %s", burnedAt, after)
	})

	t.Run("an already-expired token is left untouched", func(t *testing.T) {
		assert.Nil(t, usedAtOf(t, pool, alreadyExpired.Id),
			"an expired token was never active, so nothing had to be invalidated")
	})

	t.Run("another user's token is untouched", func(t *testing.T) {
		found, err := repository.FindByDigest(ctx, spared.TokenDigest)
		require.NoError(t, err)

		assert.Nil(t, found.UsedAt, "one user's reset request must never reach another account")
		assert.NoError(t, found.Validate(time.Now()))
	})
}

func TestPasswordResetTokenRepositoryInvalidateActiveForUserWithoutTokens(t *testing.T) {
	pool := newTestPool(t)

	user := createResetTestUser(t, pool)

	assert.NoError(t, NewPasswordResetTokenRepository(pool).InvalidateActiveForUser(userTestContext(t), user.Id),
		"a user who never requested a reset has nothing to invalidate")
}
