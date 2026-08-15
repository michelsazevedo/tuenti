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

const emailConfirmationTokenTTL = 24 * time.Hour

func createConfirmationTestUser(t *testing.T, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	user := createTestUser(t, pool)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), userTestTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM email_confirmation_tokens WHERE user_id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for the email confirmation tokens of user %v: %v", user.Id, err)
		}
	})

	return user
}

func seedEmailConfirmationToken(
	t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID, expiresAt time.Time,
) (string, *domain.EmailConfirmationToken) {
	t.Helper()

	rawToken, err := domain.GenerateEmailConfirmationToken()
	require.NoError(t, err)

	token := &domain.EmailConfirmationToken{
		UserID:      userID,
		TokenDigest: domain.HashEmailConfirmationToken(rawToken),
		ExpiresAt:   expiresAt,
	}
	require.NoError(t, NewEmailConfirmationTokenRepository(pool).Create(userTestContext(t), token))

	return rawToken, token
}

func confirmationUsedAtOf(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) *time.Time {
	t.Helper()

	var usedAt *time.Time

	require.NoError(t, pool.QueryRow(
		userTestContext(t), `SELECT used_at FROM email_confirmation_tokens WHERE id = $1`, id,
	).Scan(&usedAt))

	return usedAt
}

func TestEmailConfirmationTokenRepositoryCreateAndFindByDigest(t *testing.T) {
	pool := newTestPool(t)
	repository := NewEmailConfirmationTokenRepository(pool)
	ctx := userTestContext(t)

	user := createConfirmationTestUser(t, pool)
	expiresAt := time.Now().Add(emailConfirmationTokenTTL)

	rawToken, created := seedEmailConfirmationToken(t, pool, user.Id, expiresAt)

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
			"storing the raw token would let a database leak confirm every account")
		assert.NotContains(t, found.TokenDigest, rawToken)

		token, err := repository.FindByDigest(ctx, rawToken)

		assert.Nil(t, token, "the raw token must not itself be a lookup key")
		assert.ErrorIs(t, err, domain.ErrEmailConfirmationTokenInvalid)
	})

	t.Run("the token is still redeemable", func(t *testing.T) {
		assert.False(t, found.IsUsed())
		assert.False(t, found.IsExpired(time.Now()))
	})
}

func TestEmailConfirmationTokenRepositoryFindByDigestUnknown(t *testing.T) {
	pool := newTestPool(t)

	token, err := NewEmailConfirmationTokenRepository(pool).FindByDigest(
		userTestContext(t), domain.HashEmailConfirmationToken("never-issued-"+randomSuffix(t)),
	)

	assert.Nil(t, token)
	assert.ErrorIs(t, err, domain.ErrEmailConfirmationTokenInvalid,
		"an unknown digest is an invalid token, not a bare lookup miss")
}

func TestEmailConfirmationTokenRepositoryFindByDigestIgnoresOtherTokenFamilies(t *testing.T) {
	pool := newTestPool(t)
	ctx := userTestContext(t)

	user := createConfirmationTestUser(t, pool)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), userTestTimeout)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for the password reset tokens of user %v: %v", user.Id, err)
		}
	})

	_, reset := seedPasswordResetToken(t, pool, user.Id, time.Now().Add(passwordResetTokenTTL))

	token, err := NewEmailConfirmationTokenRepository(pool).FindByDigest(ctx, reset.TokenDigest)

	assert.Nil(t, token, "a password reset token must never confirm an email address")
	assert.ErrorIs(t, err, domain.ErrEmailConfirmationTokenInvalid)
}

func TestEmailConfirmationTokenRepositoryMarkUsed(t *testing.T) {
	pool := newTestPool(t)
	repository := NewEmailConfirmationTokenRepository(pool)
	ctx := userTestContext(t)

	user := createConfirmationTestUser(t, pool)
	_, created := seedEmailConfirmationToken(t, pool, user.Id, time.Now().Add(emailConfirmationTokenTTL))

	require.NoError(t, repository.MarkUsed(ctx, created.Id))

	burned, err := repository.FindByDigest(ctx, created.TokenDigest)
	require.NoError(t, err)

	require.NotNil(t, burned.UsedAt, "MarkUsed must stamp used_at")
	assert.WithinDuration(t, time.Now(), *burned.UsedAt, time.Minute)
	assert.True(t, burned.IsUsed(), "a burned token must report itself as used")

	t.Run("marking it again reports the race instead of restamping it", func(t *testing.T) {
		time.Sleep(50 * time.Millisecond)

		assert.ErrorIs(t, repository.MarkUsed(ctx, created.Id), domain.ErrEmailConfirmationTokenUsed,
			"a retry must report that the token was already burned, not silently succeed")

		again, err := repository.FindByDigest(ctx, created.TokenDigest)
		require.NoError(t, err)
		require.NotNil(t, again.UsedAt)

		assert.True(t, burned.UsedAt.Equal(*again.UsedAt),
			"used_at must keep reporting when the confirmation happened: was %s, now %s",
			burned.UsedAt, again.UsedAt)
	})
}

func TestEmailConfirmationTokenRepositoryMarkUsedForUnknownToken(t *testing.T) {
	pool := newTestPool(t)

	unknown := pgtype.UUID{Valid: true}
	copy(unknown.Bytes[:], []byte(randomSuffix(t)))

	assert.ErrorIs(t, NewEmailConfirmationTokenRepository(pool).MarkUsed(userTestContext(t), unknown),
		domain.ErrEmailConfirmationTokenUsed,
		"an id that matches no row must report it, not silently succeed")
}

func TestEmailConfirmationTokenRepositoryInvalidateActiveForUser(t *testing.T) {
	pool := newTestPool(t)
	repository := NewEmailConfirmationTokenRepository(pool)
	ctx := userTestContext(t)

	target := createConfirmationTestUser(t, pool)
	bystander := createConfirmationTestUser(t, pool)

	_, firstActive := seedEmailConfirmationToken(t, pool, target.Id, time.Now().Add(emailConfirmationTokenTTL))
	_, secondActive := seedEmailConfirmationToken(t, pool, target.Id, time.Now().Add(emailConfirmationTokenTTL))
	_, alreadyUsed := seedEmailConfirmationToken(t, pool, target.Id, time.Now().Add(emailConfirmationTokenTTL))
	_, alreadyExpired := seedEmailConfirmationToken(t, pool, target.Id, time.Now().Add(-time.Hour))
	_, spared := seedEmailConfirmationToken(t, pool, bystander.Id, time.Now().Add(emailConfirmationTokenTTL))

	require.NoError(t, repository.MarkUsed(ctx, alreadyUsed.Id))

	burnedAt := confirmationUsedAtOf(t, pool, alreadyUsed.Id)
	require.NotNil(t, burnedAt)

	time.Sleep(50 * time.Millisecond)

	require.NoError(t, repository.InvalidateActiveForUser(ctx, target.Id))

	for name, token := range map[string]*domain.EmailConfirmationToken{
		"first active":  firstActive,
		"second active": secondActive,
	} {
		found, err := repository.FindByDigest(ctx, token.TokenDigest)
		require.NoError(t, err)

		assert.NotNil(t, found.UsedAt, "the %s token outlived the resent confirmation", name)
		assert.True(t, found.IsUsed(), "the %s token must no longer be redeemable", name)
	}

	t.Run("an already-used token keeps its original instant", func(t *testing.T) {
		after := confirmationUsedAtOf(t, pool, alreadyUsed.Id)
		require.NotNil(t, after)

		assert.True(t, burnedAt.Equal(*after),
			"restamping would misreport the confirmation as happening now: was %s, now %s", burnedAt, after)
	})

	t.Run("an already-expired token is left untouched", func(t *testing.T) {
		assert.Nil(t, confirmationUsedAtOf(t, pool, alreadyExpired.Id),
			"an expired token was never active, so nothing had to be invalidated")
	})

	t.Run("another user's token is untouched", func(t *testing.T) {
		found, err := repository.FindByDigest(ctx, spared.TokenDigest)
		require.NoError(t, err)

		assert.Nil(t, found.UsedAt, "one user's resend must never reach another account")
		assert.False(t, found.IsUsed())
		assert.False(t, found.IsExpired(time.Now()))
	})
}

func TestEmailConfirmationTokenRepositoryInvalidateActiveForUserWithoutTokens(t *testing.T) {
	pool := newTestPool(t)

	user := createConfirmationTestUser(t, pool)

	assert.NoError(t, NewEmailConfirmationTokenRepository(pool).InvalidateActiveForUser(userTestContext(t), user.Id),
		"a user who was never issued a confirmation token has nothing to invalidate")
}
