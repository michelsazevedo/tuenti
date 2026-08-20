package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	orgpersistence "github.com/michelsazevedo/tuenti/internal/core/organization/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	signupTestTimeout       = 10 * time.Second
	signupConfirmBaseURL    = "https://app.example.com/confirm-email"
	signupTestEmployeeCount = 10
)

type recordingConfirmationEventPublisher struct {
	publishErr error
	published  []domain.EmailConfirmationRequested
}

func (p *recordingConfirmationEventPublisher) PublishEmailConfirmationRequested(
	_ context.Context,
	event domain.EmailConfirmationRequested,
) error {
	p.published = append(p.published, event)

	return p.publishErr
}

func (p *recordingConfirmationEventPublisher) only(t *testing.T) domain.EmailConfirmationRequested {
	t.Helper()

	require.Len(t, p.published, 1, "exactly one email confirmation event must have been published")

	return p.published[0]
}

func requireConfirmationEventMatches(
	t *testing.T,
	published domain.EmailConfirmationRequested,
	user *domain.User,
	confirmationURL string,
) {
	t.Helper()

	assert.Equal(t, domain.EventEmailConfirmationRequested, published.Event,
		"the envelope must carry the email confirmation event name")
	assert.Equal(t, user.Id.String(), published.UserID, "the event must identify the user being confirmed")
	assert.Equal(t, user.Name, published.Name, "the event must carry the name the notification will greet")
	assert.Equal(t, user.Email, published.Email, "the event must carry the address being confirmed")
	assert.Equal(t, confirmationURL, published.ConfirmationURL,
		"the event must carry the same link that was emailed")
}

func newSignup(
	pgConn *database.PgConn,
	publisher domain.ConfirmationEventPublisher,
) SignupUseCase {
	return NewSignup(
		database.NewUnitOfWork(pgConn),
		publisher,
		orgpersistence.NewIndustryRepository(pgConn.Pool()),
		&config.Config{Settings: config.Settings{EmailConfirmationBaseURL: signupConfirmBaseURL}},
	)
}

func rawTokenFromConfirmationURL(t *testing.T, confirmationURL string) string {
	t.Helper()

	require.Contains(t, confirmationURL, signupConfirmBaseURL+"?token=",
		"the link must be the configured base URL carrying the token as a query parameter")

	parsed, err := url.Parse(confirmationURL)
	require.NoError(t, err, "the confirmation link must be a parseable URL")

	rawToken := parsed.Query().Get("token")
	require.NotEmpty(t, rawToken, "the link must carry a non-empty token")

	return rawToken
}

func deleteConfirmationTokens(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
	defer cancel()

	if _, err := pool.Exec(ctx,
		`DELETE FROM email_confirmation_tokens WHERE user_id = $1`, userID); err != nil {
		t.Errorf("cleanup of email confirmation tokens failed: %v", err)
	}
}

func deleteSignupRows(t *testing.T, pool *pgxpool.Pool, userID, organizationID, membershipID pgtype.UUID) {
	t.Helper()

	deleteConfirmationTokens(t, pool, userID)

	ctx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
	defer cancel()

	for _, statement := range []struct {
		sql string
		id  pgtype.UUID
	}{
		{`DELETE FROM memberships WHERE id = $1`, membershipID},
		{`DELETE FROM organizations WHERE id = $1`, organizationID},
		{`DELETE FROM users WHERE id = $1`, userID},
	} {
		if _, err := pool.Exec(ctx, statement.sql, statement.id); err != nil {
			t.Errorf("cleanup failed for %q: %v", statement.sql, err)
		}
	}
}

func createTestIndustry(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
	defer cancel()

	suffix := randomSuffix(t)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Aerospace "+suffix, "aerospace_"+suffix,
	).Scan(&id)
	require.NoError(t, err, "the test industry must be insertable")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM industries WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup of the test industry failed: %v", err)
		}
	})

	return id
}

func newSignupOrganization(t *testing.T, pool *pgxpool.Pool, suffix string) SignupOrganization {
	t.Helper()

	return SignupOrganization{
		Name:              "Acme " + suffix,
		IndustryID:        createTestIndustry(t, pool),
		NumberOfEmployees: signupTestEmployeeCount,
	}
}

type ownerMembership struct {
	id             pgtype.UUID
	organizationID pgtype.UUID
	role           orgdomain.Role
}

func requireOwnerMembership(t *testing.T, pool *pgxpool.Pool, userID pgtype.UUID, organizationName string) ownerMembership {
	t.Helper()

	ctx := testContext(t)

	var membership ownerMembership

	err := pool.QueryRow(ctx, `
		SELECT m.id, m.organization_id, m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND o.name = $2
	`, userID, organizationName).Scan(&membership.id, &membership.organizationID, &membership.role)
	require.NoError(t, err, "signup must create exactly one membership for the new user")

	return membership
}

func newTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	return pgConn
}

func setDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func randomUUID(t *testing.T) pgtype.UUID {
	t.Helper()

	id := pgtype.UUID{Valid: true}
	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	return id
}

func randomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func TestSignUpCreatesUserOrganizationAndOwnerMembership(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:     "Wile E. Coyote " + suffix,
		Email:    "wile." + suffix + "@example.com",
		Password: "supersecret",
	}
	org := newSignupOrganization(t, pool, suffix)

	require.NoError(t, newSignup(pgConn, &recordingConfirmationEventPublisher{}).
		SignUp(ctx, user, org))

	require.True(t, user.Id.Valid, "the user id must be returned by the insert")

	membership := requireOwnerMembership(t, pool, user.Id, org.Name)

	t.Cleanup(func() {
		deleteSignupRows(t, pool, user.Id, membership.organizationID, membership.id)
	})

	assert.Equal(t, orgdomain.RoleManager, membership.role, "the first membership must own the organization")

	var (
		industryID        pgtype.UUID
		numberOfEmployees int
	)

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT industry_id, number_of_employees FROM organizations WHERE id = $1`,
		membership.organizationID).Scan(&industryID, &numberOfEmployees))

	assert.Equal(t, org.IndustryID, industryID, "the organization must reference the submitted industry")
	assert.Equal(t, org.NumberOfEmployees, numberOfEmployees,
		"the organization must store the submitted employee count")

	var memberships int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM memberships WHERE user_id = $1`, user.Id).Scan(&memberships))
	assert.Equal(t, 1, memberships, "signup must create exactly one membership")

	assert.Empty(t, user.Password, "the plaintext password must not survive signup")
	assert.NotEmpty(t, user.PasswordDigest)
}

func TestSignUpCreatesAConfirmationTokenAndPublishesTheConfirmationEvent(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:     "Marvin Martian " + suffix,
		Email:    "marvin." + suffix + "@example.com",
		Password: "supersecret",
	}
	org := newSignupOrganization(t, pool, suffix)

	publisher := &recordingConfirmationEventPublisher{}

	require.NoError(t, newSignup(pgConn, publisher).SignUp(ctx, user, org))
	require.True(t, user.Id.Valid, "the user id must be returned by the insert")

	membership := requireOwnerMembership(t, pool, user.Id, org.Name)

	t.Cleanup(func() {
		deleteSignupRows(t, pool, user.Id, membership.organizationID, membership.id)
	})

	var tokens int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM email_confirmation_tokens WHERE user_id = $1`, user.Id).Scan(&tokens))
	require.Equal(t, 1, tokens, "signup must issue exactly one confirmation token")

	var (
		tokenDigest string
		expiresAt   time.Time
		usedAt      *time.Time
		createdAt   time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT token_digest, expires_at, used_at, created_at
		FROM email_confirmation_tokens
		WHERE user_id = $1
	`, user.Id).Scan(&tokenDigest, &expiresAt, &usedAt, &createdAt))

	assert.Nil(t, usedAt, "a freshly issued token must not be marked as used")

	assert.WithinDuration(t, createdAt.Add(24*time.Hour), expiresAt, 5*time.Second,
		"the confirmation token must live for 24 hours from its creation")

	published := publisher.only(t)
	assert.True(t, strings.HasPrefix(published.ConfirmationURL, signupConfirmBaseURL+"?token="),
		"the link must be built from the configured base URL, got %q", published.ConfirmationURL)
	requireConfirmationEventMatches(t, published, user, published.ConfirmationURL)

	rawToken := rawTokenFromConfirmationURL(t, published.ConfirmationURL)
	assert.NotEqual(t, rawToken, tokenDigest, "the raw token must never be stored")
	assert.Equal(t, domain.HashEmailConfirmationToken(rawToken), tokenDigest,
		"the emailed token must hash to the stored digest")
}

func TestSignUpSucceedsEvenWhenTheConfirmationEventFailsToPublish(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:     "Elmer Fudd " + suffix,
		Email:    "elmer." + suffix + "@example.com",
		Password: "supersecret",
	}
	org := newSignupOrganization(t, pool, suffix)

	publisher := &recordingConfirmationEventPublisher{publishErr: errors.New("kafka unavailable")}

	require.NoError(t, newSignup(pgConn, publisher).SignUp(ctx, user, org),
		"a failing event publisher must not fail the signup")
	require.True(t, user.Id.Valid, "the user id must be returned by the insert")

	membership := requireOwnerMembership(t, pool, user.Id, org.Name)

	t.Cleanup(func() {
		deleteSignupRows(t, pool, user.Id, membership.organizationID, membership.id)
	})

	published := publisher.only(t)
	requireConfirmationEventMatches(t, published, user, published.ConfirmationURL)

	rawToken := rawTokenFromConfirmationURL(t, published.ConfirmationURL)

	stored, err := persistence.NewEmailConfirmationTokenRepository(pool).
		FindByDigest(ctx, domain.HashEmailConfirmationToken(rawToken))
	require.NoError(t, err, "the token must survive a publish failure so the emailed link still works")
	assert.Nil(t, stored.UsedAt)
}

func TestSignUpRollsBackEverythingWhenTheMembershipInsertFails(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:     "Road Runner " + suffix,
		Email:    "road." + suffix + "@example.com",
		Password: "supersecret",
	}
	org := newSignupOrganization(t, pool, suffix)

	withForcedMembershipInsertFailure(t, pool, org.Name)

	publisher := &recordingConfirmationEventPublisher{}

	err := newSignup(pgConn, publisher).SignUp(ctx, user, org)
	require.Error(t, err, "a failing membership insert must fail the whole signup")

	assert.Empty(t, publisher.published, "a rolled back signup must never announce a live confirmation link")

	_, err = persistence.NewUserRepository(pool).FindByEmail(ctx, user.Email)
	assert.ErrorIs(t, err, domain.ErrUserNotFound, "the user insert must have been rolled back")

	var organizations int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM organizations WHERE name = $1`, org.Name).Scan(&organizations))
	assert.Zero(t, organizations, "the organization insert must have been rolled back")
}

func TestSignUpFailsWhenTheIndustryDoesNotExist(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:     "Daffy Duck " + suffix,
		Email:    "daffy." + suffix + "@example.com",
		Password: "supersecret",
	}
	org := SignupOrganization{
		Name:              "Acme " + suffix,
		IndustryID:        randomUUID(t),
		NumberOfEmployees: signupTestEmployeeCount,
	}

	publisher := &recordingConfirmationEventPublisher{}

	err := newSignup(pgConn, publisher).SignUp(ctx, user, org)
	assert.ErrorIs(t, err, orgdomain.ErrIndustryNotFound,
		"an unknown industry must be rejected as a domain error, not a foreign key violation")

	assert.Empty(t, publisher.published, "a rejected signup must never announce a live confirmation link")

	_, err = persistence.NewUserRepository(pool).FindByEmail(ctx, user.Email)
	assert.ErrorIs(t, err, domain.ErrUserNotFound, "the user must never be inserted")

	var users int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE email = $1`, user.Email).Scan(&users))
	assert.Zero(t, users, "no user row may survive a rejected signup")

	var organizations int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM organizations WHERE name = $1`, org.Name).Scan(&organizations))
	assert.Zero(t, organizations, "no organization row may survive a rejected signup")
}

func withForcedMembershipInsertFailure(t *testing.T, pool *pgxpool.Pool, organizationName string) {
	t.Helper()

	ctx := context.Background()

	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS test_membership_failure_target(name TEXT PRIMARY KEY)`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO test_membership_failure_target(name) VALUES($1)`, organizationName)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION test_fail_membership_insert() RETURNS trigger AS $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM organizations o
				JOIN test_membership_failure_target t ON t.name = o.name
				WHERE o.id = NEW.organization_id
			) THEN
				RAISE EXCEPTION 'forced failure for rollback test';
			END IF;

			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER test_fail_membership_insert_trigger
			BEFORE INSERT ON memberships
			FOR EACH ROW EXECUTE FUNCTION test_fail_membership_insert();
	`)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DROP TRIGGER IF EXISTS test_fail_membership_insert_trigger ON memberships;
			DROP FUNCTION IF EXISTS test_fail_membership_insert();
			DROP TABLE IF EXISTS test_membership_failure_target;
		`)
	})
}
