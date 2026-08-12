package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	identity "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const testTimeout = 5 * time.Second

// newTestPool opens a real connection against the local Postgres. The pool
// satisfies database.DBTX, so the repositories are exercised through the very
// same interface they get in production (pool now, pgx.Tx inside a UnitOfWork).
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	// config.NewConfig expands the POSTGRES_* variables into the embedded
	// config.yaml; outside docker compose they are unset, so fall back to the
	// credentials the compose `db` service publishes on localhost.
	setDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setDefaultEnv(t, "POSTGRES_DB", "tuenti")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	requireSchema(t, pgConn.Pool())

	return pgConn.Pool()
}

// requireSchema fails with an actionable message instead of a cryptic scan
// error when the migrations have never been applied to this database.
func requireSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	for _, table := range []string{"organizations", "memberships", "users"} {
		var name *string

		err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, "public."+table).Scan(&name)
		require.NoError(t, err)
		require.NotNil(t, name, "table %q is missing: run the migrations (`make run` applies them at boot)", table)
	}
}

func setDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

// createTestOrganization persists an organization and schedules its deletion.
// Cleanups run LIFO, so rows created later (memberships) are removed first,
// which keeps the teardown foreign-key safe.
func createTestOrganization(t *testing.T, pool *pgxpool.Pool) *domain.Organization {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	org := &domain.Organization{Name: "Acme " + randomSuffix(t)}
	require.NoError(t, NewOrganizationRepository(pool).Create(ctx, org))

	deleteRow(t, pool, `DELETE FROM organizations WHERE id = $1`, org.Id)

	return org
}

func createTestUser(t *testing.T, pool *pgxpool.Pool) *identity.User {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	suffix := randomSuffix(t)
	user := &identity.User{
		Name:           "Wile E. Coyote " + suffix,
		Email:          "wile." + suffix + "@example.com",
		PasswordDigest: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcrypt",
	}
	require.NoError(t, persistence.NewUserRepository(pool).Create(ctx, user))

	deleteRow(t, pool, `DELETE FROM users WHERE id = $1`, user.Id)

	return user
}

func deleteRow(t *testing.T, pool *pgxpool.Pool, sql string, id pgtype.UUID) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, sql, id); err != nil {
			t.Errorf("cleanup failed for %q: %v", sql, err)
		}
	})
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
