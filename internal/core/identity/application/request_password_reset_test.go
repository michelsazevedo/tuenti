package application

import (
	"context"
	"errors"
	"net/url"
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

const requestResetBaseURL = "https://app.example.com/reset-password"

type recordingPasswordResetEventPublisher struct {
	publishErr error
	published  []domain.PasswordResetRequested
}

func (p *recordingPasswordResetEventPublisher) PublishPasswordResetRequested(
	_ context.Context,
	event domain.PasswordResetRequested,
) error {
	p.published = append(p.published, event)

	return p.publishErr
}

func (p *recordingPasswordResetEventPublisher) only(t *testing.T) domain.PasswordResetRequested {
	t.Helper()

	require.Len(t, p.published, 1, "exactly one password reset event must have been published")

	return p.published[0]
}

func requirePasswordResetEventMatches(
	t *testing.T,
	published domain.PasswordResetRequested,
	user *domain.User,
	resetLink string,
) {
	t.Helper()

	assert.Equal(t, domain.EventPasswordResetRequested, published.Event,
		"the envelope must carry the password reset event name")
	assert.Equal(t, user.Id.String(), published.UserID, "the event must identify the user resetting the password")
	assert.Equal(t, user.Name, published.Name, "the event must carry the name the notification will greet")
	assert.Equal(t, user.Email, published.Email, "the event must carry the address that was looked up")
	assert.Equal(t, resetLink, published.ResetURL, "the event must carry the same link that was emailed")
}

func newRequestPasswordReset(
	pool *pgxpool.Pool,
	publisher domain.PasswordResetEventPublisher,
) RequestPasswordResetUseCase {
	return NewRequestPasswordReset(
		persistence.NewUserRepository(pool),
		persistence.NewPasswordResetTokenRepository(pool),
		publisher,
		&config.Config{Settings: config.Settings{PasswordResetBaseURL: requestResetBaseURL}},
	)
}

func createResetTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *domain.User {
	t.Helper()

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:           "Marvin Martian " + suffix,
		Email:          "marvin." + suffix + "@example.com",
		PasswordDigest: "not-a-real-bcrypt-digest",
	}

	require.NoError(t, persistence.NewUserRepository(pool).Create(ctx, user))

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx,
			`DELETE FROM password_reset_tokens WHERE user_id = $1`, user.Id); err != nil {
			t.Errorf("cleanup of password reset tokens failed: %v", err)
		}

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, user.Id); err != nil {
			t.Errorf("cleanup of the test user failed: %v", err)
		}
	})

	return user
}

func countPasswordResetTokens(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()

	var tokens int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens`).Scan(&tokens))

	return tokens
}

func countPasswordResetTokensForUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID pgtype.UUID) int {
	t.Helper()

	var tokens int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM password_reset_tokens WHERE user_id = $1`, userID).Scan(&tokens))

	return tokens
}

func rawTokenFromLink(t *testing.T, resetLink string) string {
	t.Helper()

	require.Contains(t, resetLink, requestResetBaseURL+"?token=",
		"the link must be the configured base URL carrying the token as a query parameter")

	parsed, err := url.Parse(resetLink)
	require.NoError(t, err, "the reset link must be a parseable URL")

	rawToken := parsed.Query().Get("token")
	require.NotEmpty(t, rawToken, "the link must carry a non-empty token")

	return rawToken
}

func TestRequestPasswordResetIsSilentForAnUnknownEmail(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	publisher := &recordingPasswordResetEventPublisher{}
	tokensBefore := countPasswordResetTokens(t, ctx, pool)

	err := newRequestPasswordReset(pool, publisher).
		RequestPasswordReset(ctx, "nobody."+randomSuffix(t)+"@example.com")

	require.NoError(t, err, "an unknown address must not be distinguishable from a known one")

	assert.Empty(t, publisher.published, "no event may be published for an address with no user")
	assert.Equal(t, tokensBefore, countPasswordResetTokens(t, ctx, pool),
		"no token may be issued for an address with no user")
}

func TestRequestPasswordResetIssuesATokenAndPublishesTheRawSecret(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createResetTestUser(t, ctx, pool)
	publisher := &recordingPasswordResetEventPublisher{}

	issuedAt := time.Now()
	require.NoError(t, newRequestPasswordReset(pool, publisher).RequestPasswordReset(ctx, user.Email))

	published := publisher.only(t)
	rawToken := rawTokenFromLink(t, published.ResetURL)

	stored, err := persistence.NewPasswordResetTokenRepository(pool).
		FindByDigest(ctx, domain.HashPasswordResetToken(rawToken))
	require.NoError(t, err, "the published token must resolve to a persisted row")

	assert.Equal(t, user.Id, stored.UserID)
	assert.Nil(t, stored.UsedAt, "a freshly issued token must still be redeemable")
	assert.WithinDuration(t, issuedAt.Add(passwordResetTokenTTL), stored.ExpiresAt, time.Minute,
		"the token must expire roughly one TTL from now")

	assert.NotEqual(t, stored.TokenDigest, rawToken, "the raw token must be published, never the stored digest")
	assert.Equal(t, rawToken, url.QueryEscape(rawToken), "the raw token must be URL-safe as issued")

	assert.Equal(t, 1, countPasswordResetTokensForUser(t, ctx, pool, user.Id),
		"a single request must issue exactly one token")

	requirePasswordResetEventMatches(t, published, user, published.ResetURL)
}

func TestRequestPasswordResetInvalidatesThePreviousToken(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createResetTestUser(t, ctx, pool)
	publisher := &recordingPasswordResetEventPublisher{}
	useCase := newRequestPasswordReset(pool, publisher)

	require.NoError(t, useCase.RequestPasswordReset(ctx, user.Email))
	require.NoError(t, useCase.RequestPasswordReset(ctx, user.Email))

	require.Len(t, publisher.published, 2, "each request must produce its own event")

	firstToken := rawTokenFromLink(t, publisher.published[0].ResetURL)
	secondToken := rawTokenFromLink(t, publisher.published[1].ResetURL)
	require.NotEqual(t, firstToken, secondToken, "each request must mint a fresh secret")

	tokens := persistence.NewPasswordResetTokenRepository(pool)

	first, err := tokens.FindByDigest(ctx, domain.HashPasswordResetToken(firstToken))
	require.NoError(t, err)
	assert.NotNil(t, first.UsedAt, "the superseded token must have been burned by the second request")

	second, err := tokens.FindByDigest(ctx, domain.HashPasswordResetToken(secondToken))
	require.NoError(t, err)
	assert.Nil(t, second.UsedAt, "the token issued last must remain redeemable")
}

func TestRequestPasswordResetSucceedsWhenTheEventFailsToPublish(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	user := createResetTestUser(t, ctx, pool)
	publisher := &recordingPasswordResetEventPublisher{publishErr: errors.New("kafka: broker unreachable")}

	require.NoError(t, newRequestPasswordReset(pool, publisher).RequestPasswordReset(ctx, user.Email),
		"a publish failure must not fail the request")

	published := publisher.only(t)
	assert.Equal(t, user.Email, published.Email,
		"the reset event must still be published when only the publish acknowledgement fails")

	requirePasswordResetEventMatches(t, published, user, published.ResetURL)

	rawToken := rawTokenFromLink(t, published.ResetURL)

	stored, err := persistence.NewPasswordResetTokenRepository(pool).
		FindByDigest(ctx, domain.HashPasswordResetToken(rawToken))
	require.NoError(t, err, "the token must survive a publish failure so the emailed link still works")
	assert.Nil(t, stored.UsedAt)
}
