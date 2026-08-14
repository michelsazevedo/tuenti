package application

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
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const signupTestTimeout = 10 * time.Second

func newTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setDefaultEnv(t, "RESEND_API_KEY", "test-key")
	setDefaultEnv(t, "RESEND_FROM_EMAIL", "test@example.com")
	setDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")

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
	organizationName := "Acme " + suffix

	require.NoError(t, NewSignup(database.NewUnitOfWork(pgConn)).SignUp(ctx, user, organizationName))

	require.True(t, user.Id.Valid, "the user id must be returned by the insert")

	var (
		membershipID   pgtype.UUID
		organizationID pgtype.UUID
		role           orgdomain.Role
	)

	err := pool.QueryRow(ctx, `
		SELECT m.id, m.organization_id, m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND o.name = $2
	`, user.Id, organizationName).Scan(&membershipID, &organizationID, &role)
	require.NoError(t, err, "signup must create exactly one membership for the new user")

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), signupTestTimeout)
		defer cancel()

		for _, statement := range []struct {
			sql string
			id  pgtype.UUID
		}{
			{`DELETE FROM memberships WHERE id = $1`, membershipID},
			{`DELETE FROM organizations WHERE id = $1`, organizationID},
			{`DELETE FROM users WHERE id = $1`, user.Id},
		} {
			if _, err := pool.Exec(cleanupCtx, statement.sql, statement.id); err != nil {
				t.Errorf("cleanup failed for %q: %v", statement.sql, err)
			}
		}
	})

	assert.Equal(t, orgdomain.RoleOwner, role, "the first membership must own the organization")

	var memberships int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM memberships WHERE user_id = $1`, user.Id).Scan(&memberships))
	assert.Equal(t, 1, memberships, "signup must create exactly one membership")

	assert.Empty(t, user.Password, "the plaintext password must not survive signup")
	assert.NotEmpty(t, user.PasswordDigest)
}

func TestSignUpRollsBackEverythingWhenTheMembershipInsertFails(t *testing.T) {
	pgConn := newTestPgConn(t)
	pool := pgConn.Pool()
	ctx := testContext(t)

	withForcedMembershipInsertFailure(t, pool)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:     "Road Runner " + suffix,
		Email:    "road." + suffix + "@example.com",
		Password: "supersecret",
	}
	organizationName := "Acme " + suffix

	err := NewSignup(database.NewUnitOfWork(pgConn)).SignUp(ctx, user, organizationName)
	require.Error(t, err, "a failing membership insert must fail the whole signup")

	_, err = persistence.NewUserRepository(pool).FindByEmail(ctx, user.Email)
	assert.ErrorIs(t, err, domain.ErrUserNotFound, "the user insert must have been rolled back")

	var organizations int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM organizations WHERE name = $1`, organizationName).Scan(&organizations))
	assert.Zero(t, organizations, "the organization insert must have been rolled back")
}

func withForcedMembershipInsertFailure(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION test_fail_membership_insert() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'forced failure for rollback test'; END;
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
		`)
	})
}
