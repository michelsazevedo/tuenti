package application

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const confirmEmailPassword = "the-password-they-signed-up-with"

func newTestConfirmEmail(pgConn *database.PgConn) ConfirmEmailUseCase {
	pool := pgConn.Pool()

	return NewConfirmEmail(
		database.NewUnitOfWork(pgConn),
		persistence.NewEmailConfirmationTokenRepository(pool),
		persistence.NewUserRepository(pool),
	)
}

func createConfirmEmailUser(t *testing.T, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	digest, err := bcrypt.GenerateFromPassword([]byte(confirmEmailPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:           "Wile E. Coyote " + suffix,
		Email:          "wile." + suffix + "@example.com",
		PasswordDigest: string(digest),
	}
	require.NoError(t, persistence.NewUserRepository(pool).Create(testContext(t), user))

	require.Nil(t, confirmedAtOf(t, pool, user.Id), "a freshly created user must start out unconfirmed")

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for user %v: %v", user.Id, err)
		}
	})

	t.Cleanup(func() { deleteConfirmationTokens(t, pool, user.Id) })

	return user
}

func seedConfirmEmailToken(
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
	require.NoError(t, persistence.NewEmailConfirmationTokenRepository(pool).Create(testContext(t), token))

	return rawToken, token
}

func confirmedAtOf(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID) *time.Time {
	t.Helper()

	var confirmedAt *time.Time

	require.NoError(t, pool.QueryRow(
		testContext(t), `SELECT confirmed_at FROM users WHERE id = $1`, userID,
	).Scan(&confirmedAt))

	return confirmedAt
}

func tokenUsedAtOf(t *testing.T, pool *pgxpool.Pool, tokenID pgtype.UUID) *time.Time {
	t.Helper()

	var usedAt *time.Time

	require.NoError(t, pool.QueryRow(
		testContext(t), `SELECT used_at FROM email_confirmation_tokens WHERE id = $1`, tokenID,
	).Scan(&usedAt))

	return usedAt
}

func TestConfirmEmailConfirmsTheAccountAndBurnsTheToken(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createConfirmEmailUser(t, pool)
	rawToken, token := seedConfirmEmailToken(t, pool, user.Id, time.Now().Add(emailConfirmationTokenTTL))

	require.NoError(t, newTestConfirmEmail(pgConn).ConfirmEmail(ctx, rawToken))

	t.Run("the account is confirmed", func(t *testing.T) {
		confirmedAt := confirmedAtOf(t, pool, user.Id)

		require.NotNil(t, confirmedAt, "confirming must stamp confirmed_at on the user row")
		assert.WithinDuration(t, time.Now(), *confirmedAt, time.Minute)
	})

	t.Run("the token is burnt", func(t *testing.T) {
		usedAt := tokenUsedAtOf(t, pool, token.Id)

		require.NotNil(t, usedAt, "the redeemed link must not stay redeemable")
		assert.WithinDuration(t, time.Now(), *usedAt, time.Minute)
	})

	t.Run("the confirmation and the burn land together", func(t *testing.T) {
		assert.WithinDuration(t, *confirmedAtOf(t, pool, user.Id), *tokenUsedAtOf(t, pool, token.Id), time.Second)
	})
}

func TestConfirmEmailRejectsAnUnknownToken(t *testing.T) {
	pgConn := newTestPgConn(t)

	err := newTestConfirmEmail(pgConn).ConfirmEmail(testContext(t), "never-issued-"+randomSuffix(t))

	assert.ErrorIs(t, err, domain.ErrEmailConfirmationTokenInvalid,
		"a digest matching no row must surface unwrapped so the handler can branch on it")
}

func TestConfirmEmailRejectsAnExpiredToken(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()

	user := createConfirmEmailUser(t, pool)
	rawToken, token := seedConfirmEmailToken(t, pool, user.Id, time.Now().Add(-time.Minute))

	err := newTestConfirmEmail(pgConn).ConfirmEmail(testContext(t), rawToken)

	assert.ErrorIs(t, err, domain.ErrEmailConfirmationTokenExpired)
	assert.Nil(t, confirmedAtOf(t, pool, user.Id),
		"a link that died before it was clicked must not confirm the account")
	assert.Nil(t, tokenUsedAtOf(t, pool, token.Id),
		"a rejected token must not be burnt: the user still needs a fresh link, not a mystery")
}

func TestConfirmEmailIsIdempotentForAnAlreadyConfirmedUser(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createConfirmEmailUser(t, pool)
	rawToken, token := seedConfirmEmailToken(t, pool, user.Id, time.Now().Add(emailConfirmationTokenTTL))

	firstConfirmation := time.Now().Add(-time.Hour).UTC()
	require.NoError(t, persistence.NewUserRepository(pool).MarkConfirmed(ctx, user.Id, firstConfirmation))
	require.NoError(t, persistence.NewEmailConfirmationTokenRepository(pool).MarkUsed(ctx, token.Id))

	confirmedBefore := confirmedAtOf(t, pool, user.Id)
	require.NotNil(t, confirmedBefore)

	usedBefore := tokenUsedAtOf(t, pool, token.Id)
	require.NotNil(t, usedBefore)

	assert.NoError(t, newTestConfirmEmail(pgConn).ConfirmEmail(ctx, rawToken),
		"clicking the same link twice must land the user on a confirmed account, not on an error page")

	t.Run("the original confirmation instant survives", func(t *testing.T) {
		confirmedAfter := confirmedAtOf(t, pool, user.Id)
		require.NotNil(t, confirmedAfter)

		assert.True(t, confirmedBefore.Equal(*confirmedAfter),
			"confirmed_at must keep reporting when the address was actually confirmed: was %s, now %s",
			confirmedBefore, confirmedAfter)
	})

	t.Run("the token keeps the instant it was burnt at", func(t *testing.T) {
		usedAfter := tokenUsedAtOf(t, pool, token.Id)
		require.NotNil(t, usedAfter)

		assert.True(t, usedBefore.Equal(*usedAfter),
			"a replay must not re-burn the token: was %s, now %s", usedBefore, usedAfter)
	})
}

func TestConfirmEmailRejectsAUsedTokenWhenTheUserIsNotConfirmed(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createConfirmEmailUser(t, pool)
	rawToken, token := seedConfirmEmailToken(t, pool, user.Id, time.Now().Add(emailConfirmationTokenTTL))
	require.NoError(t, persistence.NewEmailConfirmationTokenRepository(pool).MarkUsed(ctx, token.Id))

	err := newTestConfirmEmail(pgConn).ConfirmEmail(ctx, rawToken)

	assert.ErrorIs(t, err, domain.ErrEmailConfirmationTokenUsed)
	assert.Nil(t, confirmedAtOf(t, pool, user.Id),
		"a burnt token must not be able to confirm the account it never confirmed")
}

func TestConfirmEmailReportsExpiryBeforeReplay(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createConfirmEmailUser(t, pool)
	rawToken, token := seedConfirmEmailToken(t, pool, user.Id, time.Now().Add(-time.Minute))
	require.NoError(t, persistence.NewEmailConfirmationTokenRepository(pool).MarkUsed(ctx, token.Id))

	err := newTestConfirmEmail(pgConn).ConfirmEmail(ctx, rawToken)

	assert.ErrorIs(t, err, domain.ErrEmailConfirmationTokenExpired,
		"expiry outranks the replay check: a dead link must report that it died")
	assert.NotErrorIs(t, err, domain.ErrEmailConfirmationTokenUsed,
		"reading a dead link as a second click would tell the user to sign in to an unconfirmed account")
	assert.Nil(t, confirmedAtOf(t, pool, user.Id))
}
