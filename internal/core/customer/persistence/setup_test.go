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
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const testTimeout = 5 * time.Second

func newTestRepository(t *testing.T) (*CustomerRepository, *pgxpool.Pool) {
	t.Helper()

	pool := newTestPool(t)

	return NewCustomerRepository(pool), pool
}

func newTestPool(t *testing.T) *pgxpool.Pool {
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

func requireSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	for _, table := range []string{"organizations", "industries", "customers"} {
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

// createTestOrganization inserts an organization and registers a cleanup that removes
// its customers (soft-deleted ones included) before the organization itself.
func createTestOrganization(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	industryID := createTestIndustry(t, pool)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO organizations(name, industry_id, number_of_employees) VALUES($1, $2, $3) RETURNING id`,
		"Acme "+randomSuffix(t), industryID, 1,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM customers WHERE organization_id = $1`, id); err != nil {
			t.Errorf("cleanup failed for customers: %v", err)
		}

		if _, err := pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup failed for organizations: %v", err)
		}
	})

	return id
}

func createTestIndustry(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	suffix := randomSuffix(t)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Customer Test Industry "+suffix, "customer_test_industry_"+suffix,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM industries WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup failed for industries: %v", err)
		}
	})

	return id
}

func newTestCustomer(t *testing.T, organizationID pgtype.UUID) *domain.Customer {
	t.Helper()

	suffix := randomSuffix(t)

	return &domain.Customer{
		OrganizationID: organizationID,
		Name:           "Wile E. Coyote " + suffix,
		CompanyName:    "Acme Corp " + suffix,
		Email:          "wile." + suffix + "@example.com",
		PhoneNumber:    "+1 555 0100",
	}
}

func fullTestAddress() *domain.Address {
	return &domain.Address{
		Street:     "1 Desert Rd",
		Complement: "Canyon 3",
		City:       "Phoenix",
		State:      "AZ",
		Country:    "US",
	}
}

func customerIDs(customers []*domain.Customer) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(customers))
	for _, customer := range customers {
		ids = append(ids, customer.ID)
	}

	return ids
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
