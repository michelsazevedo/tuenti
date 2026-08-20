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
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createItemTestTimeout = 10 * time.Second

func newCreateItemUseCase(t *testing.T, pgConn *database.PgConn) CreateItemUseCase {
	t.Helper()

	return NewCreateItem(database.NewUnitOfWork(pgConn))
}

func newCreateItemTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setCreateItemDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setCreateItemDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setCreateItemDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setCreateItemDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setCreateItemDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setCreateItemDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setCreateItemDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), createItemTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	requireCreateItemSchema(t, pgConn.Pool())

	return pgConn
}

func requireCreateItemSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), createItemTestTimeout)
	defer cancel()

	for _, table := range []string{"industries", "organizations", "items", "item_taxes"} {
		var name *string

		err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, "public."+table).Scan(&name)
		require.NoError(t, err)
		require.NotNil(t, name, "table %q is missing: run the migrations (`make run` applies them at boot)", table)
	}
}

func setCreateItemDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func createItemRandomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func createItemTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), createItemTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func createItemTestOrganization(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), createItemTestTimeout)
	defer cancel()

	industryID := createItemTestIndustry(t, pool)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO organizations(name, industry_id, number_of_employees) VALUES($1, $2, $3) RETURNING id`,
		"Acme "+createItemRandomSuffix(t), industryID, 1,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), createItemTestTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM items WHERE organization_id = $1`, id); err != nil {
			t.Errorf("cleanup failed for items: %v", err)
		}

		if _, err := pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup failed for organizations: %v", err)
		}
	})

	return id
}

func createItemTestIndustry(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), createItemTestTimeout)
	defer cancel()

	suffix := createItemRandomSuffix(t)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Create Item Test Industry "+suffix, "create_item_test_industry_"+suffix,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), createItemTestTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM industries WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup failed for industries: %v", err)
		}
	})

	return id
}

func newCreateItemInput(t *testing.T, organizationID pgtype.UUID) *domain.Item {
	t.Helper()

	return &domain.Item{
		OrganizationID:  organizationID,
		Name:            "Acme Anvil " + createItemRandomSuffix(t),
		Description:     "Heavy",
		Type:            domain.ItemTypeItem,
		Rate:            decimal.RequireFromString("1999.99"),
		Currency:        "USD",
		IncomeAccount:   "4000",
		TrackInventory:  true,
		QuantityInStock: 10,
	}
}

func countCreateItemRows(t *testing.T, pool *pgxpool.Pool, organizationID pgtype.UUID) int {
	t.Helper()

	var items int

	require.NoError(t, pool.QueryRow(createItemTestContext(t),
		`SELECT count(*) FROM items WHERE organization_id = $1`, organizationID).Scan(&items))

	return items
}

func TestCreateItemPersistsAnItemWithItsTaxes(t *testing.T) {
	pgConn := newCreateItemTestPgConn(t)
	ctx := createItemTestContext(t)

	organizationID := createItemTestOrganization(t, pgConn.Pool())

	input := newCreateItemInput(t, organizationID)
	input.Taxes = []domain.ItemTax{
		{Name: "VAT", TaxNumber: "VAT-001", Rate: decimal.RequireFromString("7.25"), Enabled: true},
		{Name: "City", TaxNumber: "CITY-002", Rate: decimal.RequireFromString("1.50"), Enabled: false},
	}

	created, err := newCreateItemUseCase(t, pgConn).CreateItem(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, created)

	assert.True(t, created.ID.Valid, "the insert must hand back a usable id")
	assert.False(t, created.CreatedAt.IsZero(), "created_at must be hydrated from the database")
	assert.False(t, created.UpdatedAt.IsZero(), "updated_at must be hydrated from the database")

	require.Len(t, created.Taxes, 2)

	for _, tax := range created.Taxes {
		assert.True(t, tax.ID.Valid, "every child tax must come back with its own generated id")
		assert.Equal(t, created.ID, tax.ItemID, "taxes must be wired to their parent item")
		assert.False(t, tax.CreatedAt.IsZero())
	}

	assert.NotEqual(t, created.Taxes[0].ID, created.Taxes[1].ID, "each tax must get a distinct id")

	found, err := persistence.NewItemRepository(pgConn.Pool()).FindByID(ctx, organizationID, created.ID)
	require.NoError(t, err, "the committed item must be readable outside its transaction")

	assert.Equal(t, input.Name, found.Name)
	assert.Equal(t, "1999.99", found.Rate.String(), "money must survive NUMERIC without drifting through a float")
	require.Len(t, found.Taxes, 2, "both tax rows must have been committed with the item")
}

func TestCreateItemNormalizesAServiceToAStocklessShape(t *testing.T) {
	pgConn := newCreateItemTestPgConn(t)
	ctx := createItemTestContext(t)

	organizationID := createItemTestOrganization(t, pgConn.Pool())

	input := newCreateItemInput(t, organizationID)
	input.Type = domain.ItemTypeService
	input.TrackInventory = true
	input.QuantityInStock = 5

	created, err := newCreateItemUseCase(t, pgConn).CreateItem(ctx, input)

	require.NoError(t, err, "a service requesting stock is normalized, not rejected")
	require.NotNil(t, created)

	assert.False(t, created.TrackInventory, "a service must never track inventory")
	assert.Zero(t, created.QuantityInStock, "a service must never carry stock")

	found, err := persistence.NewItemRepository(pgConn.Pool()).FindByID(ctx, organizationID, created.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.ItemTypeService, found.Type)
	assert.False(t, found.TrackInventory,
		"normalization must reach the database, not just the in-memory aggregate")
	assert.Zero(t, found.QuantityInStock,
		"the persisted row must not disagree with the type about stock")
}

func TestCreateItemRejectsInvalidAggregatesWithoutWriting(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(item *domain.Item)
		wantErr error
	}{
		{
			name:    "an empty name",
			mutate:  func(item *domain.Item) { item.Name = "" },
			wantErr: domain.ErrNameRequired,
		},
		{
			name:    "a blank name",
			mutate:  func(item *domain.Item) { item.Name = "   " },
			wantErr: domain.ErrNameRequired,
		},
		{
			name:    "a zero rate",
			mutate:  func(item *domain.Item) { item.Rate = decimal.Zero },
			wantErr: domain.ErrInvalidRate,
		},
		{
			name:    "a negative rate",
			mutate:  func(item *domain.Item) { item.Rate = decimal.RequireFromString("-1") },
			wantErr: domain.ErrInvalidRate,
		},
		{
			name: "a tax rate above one hundred",
			mutate: func(item *domain.Item) {
				item.Taxes = []domain.ItemTax{
					{Name: "Bogus", Rate: decimal.RequireFromString("150"), Enabled: true},
				}
			},
			wantErr: domain.ErrInvalidTaxRate,
		},
		{
			name: "a negative tax rate",
			mutate: func(item *domain.Item) {
				item.Taxes = []domain.ItemTax{
					{Name: "Bogus", Rate: decimal.RequireFromString("-0.01"), Enabled: true},
				}
			},
			wantErr: domain.ErrInvalidTaxRate,
		},
		{
			name:    "an unknown type",
			mutate:  func(item *domain.Item) { item.Type = domain.ItemType("WIDGET") },
			wantErr: domain.ErrInvalidItemType,
		},
		{
			name:    "an empty type",
			mutate:  func(item *domain.Item) { item.Type = "" },
			wantErr: domain.ErrInvalidItemType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgConn := newCreateItemTestPgConn(t)
			ctx := createItemTestContext(t)

			organizationID := createItemTestOrganization(t, pgConn.Pool())

			input := newCreateItemInput(t, organizationID)
			tt.mutate(input)

			created, err := newCreateItemUseCase(t, pgConn).CreateItem(ctx, input)

			require.ErrorIs(t, err, tt.wantErr, "the use case must surface the violated domain rule verbatim")
			assert.Nil(t, created, "a rejected aggregate must not be handed back as if it were created")
			assert.Zero(t, countCreateItemRows(t, pgConn.Pool(), organizationID),
				"validation must run before the transaction, so no row may exist")
		})
	}
}

func TestCreateItemRollsBackAPartiallyWrittenAggregate(t *testing.T) {
	pgConn := newCreateItemTestPgConn(t)
	ctx := createItemTestContext(t)

	organizationID := createItemTestOrganization(t, pgConn.Pool())
	duplicateID := createItemRandomUUID(t)

	input := newCreateItemInput(t, organizationID)
	input.Taxes = []domain.ItemTax{
		{ID: duplicateID, Name: "VAT", Rate: decimal.RequireFromString("7.25"), Enabled: true},
		{ID: duplicateID, Name: "City", Rate: decimal.RequireFromString("1.50"), Enabled: true},
	}

	created, err := newCreateItemUseCase(t, pgConn).CreateItem(ctx, input)

	require.Error(t, err, "a duplicate tax id must fail the second insert")
	assert.Nil(t, created)

	assert.Zero(t, countCreateItemRows(t, pgConn.Pool(), organizationID),
		"the item row written before the failing tax must be rolled back, not left orphaned")

	var taxes int

	require.NoError(t, pgConn.Pool().QueryRow(ctx,
		`SELECT count(*) FROM item_taxes WHERE id = $1`, duplicateID).Scan(&taxes))
	assert.Zero(t, taxes, "the first tax must not survive the failure of the second")
}

func createItemRandomUUID(t *testing.T) pgtype.UUID {
	t.Helper()

	id := pgtype.UUID{Valid: true}
	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	return id
}

func TestCreateItemAcceptsTaxRateBoundaries(t *testing.T) {
	for _, rate := range []string{"0", "100"} {
		t.Run("a tax rate of "+rate, func(t *testing.T) {
			pgConn := newCreateItemTestPgConn(t)
			ctx := createItemTestContext(t)

			organizationID := createItemTestOrganization(t, pgConn.Pool())

			input := newCreateItemInput(t, organizationID)
			input.Taxes = []domain.ItemTax{
				{Name: "Boundary", TaxNumber: "B-001", Rate: decimal.RequireFromString(rate), Enabled: true},
			}

			created, err := newCreateItemUseCase(t, pgConn).CreateItem(ctx, input)

			require.NoError(t, err, "both ends of the inclusive 0..100 range are legal tax rates")
			require.NotNil(t, created)
			require.Len(t, created.Taxes, 1)

			found, err := persistence.NewItemRepository(pgConn.Pool()).FindByID(ctx, organizationID, created.ID)
			require.NoError(t, err)

			require.Len(t, found.Taxes, 1)
			assert.True(t, found.Taxes[0].Rate.Equal(decimal.RequireFromString(rate)),
				"the boundary rate must round-trip exactly, got %s", found.Taxes[0].Rate)
		})
	}
}
