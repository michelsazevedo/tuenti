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
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const customerTestTimeout = 10 * time.Second

var (
	customerTestOrganizationID = pgtype.UUID{Bytes: [16]byte{0xaa, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	customerTestID             = pgtype.UUID{Bytes: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
)

// newUnreachableUnitOfWork returns a UnitOfWork with no connection behind it.
// Any attempt to open a transaction dereferences a nil pool and panics, so a
// test that passes with it proves the use case never reached persistence.
func newUnreachableUnitOfWork() *database.UnitOfWork {
	return database.NewUnitOfWork(nil)
}

func newCustomerTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setCustomerDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setCustomerDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setCustomerDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setCustomerDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setCustomerDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setCustomerDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setCustomerDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	requireCustomerSchema(t, pgConn.Pool())

	return pgConn
}

func requireCustomerSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
	defer cancel()

	for _, table := range []string{"industries", "organizations", "customers"} {
		var name *string

		err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, "public."+table).Scan(&name)
		require.NoError(t, err)
		require.NotNil(t, name, "table %q is missing: run the migrations (`make run` applies them at boot)", table)
	}
}

func setCustomerDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func customerRandomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func customerRandomUUID(t *testing.T) pgtype.UUID {
	t.Helper()

	id := pgtype.UUID{Valid: true}
	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	return id
}

func customerTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func customerTestOrganization(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
	defer cancel()

	industryID := customerTestIndustry(t, pool)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO organizations(name, industry_id, number_of_employees) VALUES($1, $2, $3) RETURNING id`,
		"Acme "+customerRandomSuffix(t), industryID, 1,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
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

func customerTestIndustry(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
	defer cancel()

	suffix := customerRandomSuffix(t)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Customer Test Industry "+suffix, "customer_test_industry_"+suffix,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM industries WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup failed for industries: %v", err)
		}
	})

	return id
}

func newCustomerInput(t *testing.T, organizationID pgtype.UUID) *domain.Customer {
	t.Helper()

	suffix := customerRandomSuffix(t)

	return &domain.Customer{
		OrganizationID: organizationID,
		Name:           "Jane Doe " + suffix,
		CompanyName:    "Acme Corp",
		Email:          "jane." + suffix + "@example.com",
		PhoneNumber:    "+1 555 0100",
		Address: &domain.Address{
			Street:  "1 Main St",
			City:    "Springfield",
			State:   "IL",
			Country: "us",
		},
	}
}

// customerValidationCases are the invariant violations Create and Update must
// both reject before persistence is touched.
var customerValidationCases = []struct {
	name    string
	mutate  func(customer *domain.Customer)
	wantErr error
}{
	{name: "an empty name", mutate: func(c *domain.Customer) { c.Name = "" }, wantErr: domain.ErrNameRequired},
	{name: "a blank name", mutate: func(c *domain.Customer) { c.Name = "   " }, wantErr: domain.ErrNameRequired},
	{name: "a blank company name", mutate: func(c *domain.Customer) { c.CompanyName = " " }, wantErr: domain.ErrCompanyNameRequired},
	{name: "an invalid email", mutate: func(c *domain.Customer) { c.Email = "not-an-email" }, wantErr: domain.ErrInvalidEmail},
	{name: "a blank phone number", mutate: func(c *domain.Customer) { c.PhoneNumber = "" }, wantErr: domain.ErrPhoneNumberRequired},
	{name: "an unknown country", mutate: func(c *domain.Customer) { c.Address.Country = "XX" }, wantErr: domain.ErrInvalidCountry},
}
