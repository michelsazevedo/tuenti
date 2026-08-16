package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

func newResendConfirmation(
	pool *pgxpool.Pool,
	publisher domain.ConfirmationEventPublisher,
) ResendConfirmationUseCase {
	return NewResendConfirmation(
		persistence.NewUserRepository(pool),
		persistence.NewEmailConfirmationTokenRepository(pool),
		publisher,
		&config.Config{Settings: config.Settings{EmailConfirmationBaseURL: signupConfirmBaseURL}},
	)
}

func createUnconfirmedTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:           "Daffy Duck " + suffix,
		Email:          "daffy." + suffix + "@example.com",
		PasswordDigest: "not-a-real-bcrypt-digest",
	}

	require.NoError(t, persistence.NewUserRepository(pool).Create(ctx, user))
	require.False(t, user.IsConfirmed(), "a freshly created user must start unconfirmed")

	t.Cleanup(func() {
		deleteConfirmationTokens(t, pool, user.Id)

		cleanupCtx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, user.Id); err != nil {
			t.Errorf("cleanup of the test user failed: %v", err)
		}
	})

	return user
}

func issueConfirmationToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID pgtype.UUID) string {
	t.Helper()

	rawToken, err := domain.GenerateEmailConfirmationToken()
	require.NoError(t, err)

	require.NoError(t, persistence.NewEmailConfirmationTokenRepository(pool).Create(ctx, &domain.EmailConfirmationToken{
		UserID:      userID,
		TokenDigest: domain.HashEmailConfirmationToken(rawToken),
		ExpiresAt:   time.Now().Add(emailConfirmationTokenTTL),
	}))

	return rawToken
}

func countEmailConfirmationTokens(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()

	var tokens int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM email_confirmation_tokens`).Scan(&tokens))

	return tokens
}

func countEmailConfirmationTokensForUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID pgtype.UUID) int {
	t.Helper()

	var tokens int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM email_confirmation_tokens WHERE user_id = $1`, userID).Scan(&tokens))

	return tokens
}

func TestResendConfirmationIsSilentForAnUnknownEmail(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	publisher := &recordingConfirmationEventPublisher{}
	tokensBefore := countEmailConfirmationTokens(t, ctx, pool)

	err := newResendConfirmation(pool, publisher).
		ResendConfirmation(ctx, "nobody."+randomSuffix(t)+"@example.com")

	require.NoError(t, err, "an unknown address must not be distinguishable from a known one")

	assert.Empty(t, publisher.published, "no event may be published for an address with no user")
	assert.Equal(t, tokensBefore, countEmailConfirmationTokens(t, ctx, pool),
		"no token may be issued for an address with no user")
}

func TestResendConfirmationIsSilentForAnAlreadyConfirmedUser(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createUnconfirmedTestUser(t, ctx, pool)
	require.NoError(t, persistence.NewUserRepository(pool).MarkConfirmed(ctx, user.Id, time.Now().UTC()))

	publisher := &recordingConfirmationEventPublisher{}
	tokensBefore := countEmailConfirmationTokens(t, ctx, pool)

	require.NoError(t, newResendConfirmation(pool, publisher).ResendConfirmation(ctx, user.Email),
		"an already confirmed account must return the same nil as a real send")

	assert.Empty(t, publisher.published, "a confirmed account has nothing to confirm, so no event may go out")
	assert.Zero(t, countEmailConfirmationTokensForUser(t, ctx, pool, user.Id),
		"no token may be minted for an account that is already confirmed")
	assert.Equal(t, tokensBefore, countEmailConfirmationTokens(t, ctx, pool),
		"the already confirmed path must leave the token table untouched")
}

func TestResendConfirmationSupersedesTheActiveTokenAndPublishesTheNewSecret(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createUnconfirmedTestUser(t, ctx, pool)
	previousToken := issueConfirmationToken(t, ctx, pool, user.Id)

	publisher := &recordingConfirmationEventPublisher{}
	issuedAt := time.Now()

	require.NoError(t, newResendConfirmation(pool, publisher).ResendConfirmation(ctx, user.Email))

	published := publisher.only(t)
	rawToken := rawTokenFromConfirmationURL(t, published.ConfirmationURL)
	require.NotEqual(t, previousToken, rawToken, "the resend must mint a fresh secret")

	tokens := persistence.NewEmailConfirmationTokenRepository(pool)

	previous, err := tokens.FindByDigest(ctx, domain.HashEmailConfirmationToken(previousToken))
	require.NoError(t, err, "the superseded token must remain on record")
	assert.NotNil(t, previous.UsedAt, "the previously active token must have been burned by the resend")

	stored, err := tokens.FindByDigest(ctx, domain.HashEmailConfirmationToken(rawToken))
	require.NoError(t, err, "the published token must resolve to a persisted row")

	assert.Equal(t, user.Id, stored.UserID)
	assert.Nil(t, stored.UsedAt, "the freshly issued token must still be redeemable")
	assert.NotEqual(t, previous.TokenDigest, stored.TokenDigest, "the new row must carry a different digest")
	assert.WithinDuration(t, issuedAt.Add(emailConfirmationTokenTTL), stored.ExpiresAt, time.Minute,
		"the token must expire roughly one TTL from now")

	assert.NotEqual(t, stored.TokenDigest, rawToken, "the raw token must be published, never the stored digest")

	assert.Equal(t, 2, countEmailConfirmationTokensForUser(t, ctx, pool, user.Id),
		"the resend must add exactly one row on top of the seeded one")

	var live int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM email_confirmation_tokens WHERE user_id = $1 AND used_at IS NULL`,
		user.Id).Scan(&live))
	assert.Equal(t, 1, live, "exactly one confirmation token may be live for the user after a resend")

	requireConfirmationEventMatches(t, published, user, published.ConfirmationURL)
}

func TestResendConfirmationSucceedsWhenTheEventFailsToPublish(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createUnconfirmedTestUser(t, ctx, pool)
	publisher := &recordingConfirmationEventPublisher{publishErr: errors.New("kafka: broker unreachable")}

	require.NoError(t, newResendConfirmation(pool, publisher).ResendConfirmation(ctx, user.Email),
		"a publish failure must not fail the request")

	published := publisher.only(t)
	assert.Equal(t, user.Email, published.Email,
		"the confirmation event must still be published when only the publish acknowledgement fails")

	requireConfirmationEventMatches(t, published, user, published.ConfirmationURL)

	rawToken := rawTokenFromConfirmationURL(t, published.ConfirmationURL)

	stored, err := persistence.NewEmailConfirmationTokenRepository(pool).
		FindByDigest(ctx, domain.HashEmailConfirmationToken(rawToken))
	require.NoError(t, err, "the token must survive a publish failure so the emailed link still works")
	assert.Nil(t, stored.UsedAt)
}
