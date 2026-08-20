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

const updateItemTestTimeout = 10 * time.Second

func newUpdateItemUseCase(pgConn *database.PgConn) *UpdateItem {
	return NewUpdateItem(database.NewUnitOfWork(pgConn))
}

func newUpdateItemTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setUpdateItemDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setUpdateItemDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setUpdateItemDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setUpdateItemDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setUpdateItemDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setUpdateItemDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setUpdateItemDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), updateItemTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	requireUpdateItemSchema(t, pgConn.Pool())

	return pgConn
}

func requireUpdateItemSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), updateItemTestTimeout)
	defer cancel()

	for _, table := range []string{"industries", "organizations", "items", "item_taxes"} {
		var name *string

		err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, "public."+table).Scan(&name)
		require.NoError(t, err)
		require.NotNil(t, name, "table %q is missing: run the migrations (`make run` applies them at boot)", table)
	}
}

func setUpdateItemDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func updateItemRandomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func updateItemTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), updateItemTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func updateItemTestOrganization(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), updateItemTestTimeout)
	defer cancel()

	industryID := updateItemTestIndustry(t, pool)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO organizations(name, industry_id, number_of_employees) VALUES($1, $2, $3) RETURNING id`,
		"Acme "+updateItemRandomSuffix(t), industryID, 1,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateItemTestTimeout)
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

func updateItemTestIndustry(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), updateItemTestTimeout)
	defer cancel()

	suffix := updateItemRandomSuffix(t)

	var id pgtype.UUID

	err := pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Update Item Test Industry "+suffix, "update_item_test_industry_"+suffix,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateItemTestTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM industries WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup failed for industries: %v", err)
		}
	})

	return id
}

func updateItemFixture(t *testing.T, pgConn *database.PgConn, organizationID pgtype.UUID) *domain.Item {
	t.Helper()

	item := &domain.Item{
		OrganizationID:  organizationID,
		Name:            "Acme Anvil " + updateItemRandomSuffix(t),
		Description:     "Heavy",
		Type:            domain.ItemTypeItem,
		Rate:            decimal.RequireFromString("1999.99"),
		Currency:        "USD",
		IncomeAccount:   "4000",
		TrackInventory:  true,
		QuantityInStock: 10,
		Taxes: []domain.ItemTax{
			{Name: "VAT", TaxNumber: "VAT-001", Rate: decimal.RequireFromString("7.25"), Enabled: true},
			{Name: "City", TaxNumber: "CITY-002", Rate: decimal.RequireFromString("1.50"), Enabled: false},
		},
	}

	require.NoError(t, persistence.NewItemRepository(pgConn.Pool()).Create(updateItemTestContext(t), item))

	return item
}

func TestUpdateItemPersistsChangesAndReplacesTaxes(t *testing.T) {
	pgConn := newUpdateItemTestPgConn(t)
	ctx := updateItemTestContext(t)

	organizationID := updateItemTestOrganization(t, pgConn.Pool())
	existing := updateItemFixture(t, pgConn, organizationID)

	kept := existing.Taxes[0]
	kept.Rate = decimal.RequireFromString("8.00")

	desired := &domain.Item{
		ID:              existing.ID,
		OrganizationID:  organizationID,
		Name:            "Acme Anvil (renamed)",
		Description:     "Still heavy",
		Type:            domain.ItemTypeItem,
		Rate:            decimal.RequireFromString("2500.00"),
		Currency:        "EUR",
		IncomeAccount:   "4100",
		TrackInventory:  true,
		QuantityInStock: 25,
		Taxes: []domain.ItemTax{
			kept,
			{Name: "New Tax", TaxNumber: "NEW-1", Rate: decimal.RequireFromString("3.00"), Enabled: true},
		},
	}

	updated, err := newUpdateItemUseCase(pgConn).UpdateItem(ctx, desired)

	require.NoError(t, err)
	require.NotNil(t, updated)

	found, err := persistence.NewItemRepository(pgConn.Pool()).FindByID(ctx, organizationID, existing.ID)
	require.NoError(t, err)

	assert.Equal(t, "Acme Anvil (renamed)", found.Name)
	assert.Equal(t, "Still heavy", found.Description)
	assert.Equal(t, "2500", found.Rate.String())
	assert.Equal(t, "EUR", found.Currency)
	assert.Equal(t, "4100", found.IncomeAccount)
	assert.Equal(t, 25, found.QuantityInStock)

	require.Len(t, found.Taxes, 2, "the second tax's row must have replaced the one dropped from the request")

	names := make([]string, 0, len(found.Taxes))
	for _, tax := range found.Taxes {
		names = append(names, tax.Name)
	}
	assert.ElementsMatch(t, []string{"VAT", "New Tax"}, names, "City must be gone, New Tax must be present")

	for _, tax := range found.Taxes {
		if tax.Name == "VAT" {
			assert.Equal(t, kept.ID, tax.ID, "a tax that arrives with an id must keep it across the update")
			assert.True(t, tax.Rate.Equal(decimal.RequireFromString("8.00")))
		}
	}
}

func TestUpdateItemClearsTheTaxListWhenTheRequestHasNone(t *testing.T) {
	pgConn := newUpdateItemTestPgConn(t)
	ctx := updateItemTestContext(t)

	organizationID := updateItemTestOrganization(t, pgConn.Pool())
	existing := updateItemFixture(t, pgConn, organizationID)

	desired := &domain.Item{
		ID:              existing.ID,
		OrganizationID:  organizationID,
		Name:            existing.Name,
		Type:            domain.ItemTypeItem,
		Rate:            existing.Rate,
		Currency:        existing.Currency,
		IncomeAccount:   existing.IncomeAccount,
		TrackInventory:  existing.TrackInventory,
		QuantityInStock: existing.QuantityInStock,
	}

	_, err := newUpdateItemUseCase(pgConn).UpdateItem(ctx, desired)
	require.NoError(t, err)

	found, err := persistence.NewItemRepository(pgConn.Pool()).FindByID(ctx, organizationID, existing.ID)
	require.NoError(t, err)

	assert.Empty(t, found.Taxes)
}

func TestUpdateItemNormalizesAServiceToAStocklessShape(t *testing.T) {
	pgConn := newUpdateItemTestPgConn(t)
	ctx := updateItemTestContext(t)

	organizationID := updateItemTestOrganization(t, pgConn.Pool())
	existing := updateItemFixture(t, pgConn, organizationID)

	desired := &domain.Item{
		ID:              existing.ID,
		OrganizationID:  organizationID,
		Name:            existing.Name,
		Type:            domain.ItemTypeService,
		Rate:            existing.Rate,
		Currency:        existing.Currency,
		IncomeAccount:   existing.IncomeAccount,
		TrackInventory:  true,
		QuantityInStock: 5,
	}

	updated, err := newUpdateItemUseCase(pgConn).UpdateItem(ctx, desired)

	require.NoError(t, err, "a service requesting stock is normalized, not rejected")
	assert.False(t, updated.TrackInventory)
	assert.Zero(t, updated.QuantityInStock)

	found, err := persistence.NewItemRepository(pgConn.Pool()).FindByID(ctx, organizationID, existing.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.ItemTypeService, found.Type)
	assert.False(t, found.TrackInventory, "normalization must reach the database, not just the in-memory aggregate")
	assert.Zero(t, found.QuantityInStock)
}

func TestUpdateItemRejectsInvalidAggregatesWithoutWriting(t *testing.T) {
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
			name:    "a zero rate",
			mutate:  func(item *domain.Item) { item.Rate = decimal.Zero },
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
			name:    "an unknown type",
			mutate:  func(item *domain.Item) { item.Type = domain.ItemType("WIDGET") },
			wantErr: domain.ErrInvalidItemType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgConn := newUpdateItemTestPgConn(t)
			ctx := updateItemTestContext(t)

			organizationID := updateItemTestOrganization(t, pgConn.Pool())
			existing := updateItemFixture(t, pgConn, organizationID)

			desired := &domain.Item{
				ID:              existing.ID,
				OrganizationID:  organizationID,
				Name:            existing.Name,
				Type:            existing.Type,
				Rate:            existing.Rate,
				Currency:        existing.Currency,
				IncomeAccount:   existing.IncomeAccount,
				TrackInventory:  existing.TrackInventory,
				QuantityInStock: existing.QuantityInStock,
			}
			tt.mutate(desired)

			updated, err := newUpdateItemUseCase(pgConn).UpdateItem(ctx, desired)

			require.ErrorIs(t, err, tt.wantErr, "the use case must surface the violated domain rule verbatim")
			assert.Nil(t, updated)

			found, err := persistence.NewItemRepository(pgConn.Pool()).FindByID(ctx, organizationID, existing.ID)
			require.NoError(t, err)
			assert.Equal(t, existing.Name, found.Name, "a rejected update must not touch the persisted row")
			assert.True(t, existing.Rate.Equal(found.Rate))
		})
	}
}

func TestUpdateItemReturnsNotFoundForMissingOrCrossTenantItems(t *testing.T) {
	pgConn := newUpdateItemTestPgConn(t)
	ctx := updateItemTestContext(t)

	organizationID := updateItemTestOrganization(t, pgConn.Pool())
	otherOrganizationID := updateItemTestOrganization(t, pgConn.Pool())
	existing := updateItemFixture(t, pgConn, organizationID)

	t.Run("an id that does not exist", func(t *testing.T) {
		desired := &domain.Item{
			ID:             updateItemRandomUUID(t),
			OrganizationID: organizationID,
			Name:           "Ghost",
			Type:           domain.ItemTypeItem,
			Rate:           decimal.RequireFromString("10"),
			Currency:       "USD",
		}

		updated, err := newUpdateItemUseCase(pgConn).UpdateItem(ctx, desired)

		assert.ErrorIs(t, err, domain.ErrItemNotFound)
		assert.Nil(t, updated)
	})

	t.Run("an item owned by another organization", func(t *testing.T) {
		desired := &domain.Item{
			ID:             existing.ID,
			OrganizationID: otherOrganizationID,
			Name:           "Hijacked",
			Type:           domain.ItemTypeItem,
			Rate:           decimal.RequireFromString("10"),
			Currency:       "USD",
		}

		updated, err := newUpdateItemUseCase(pgConn).UpdateItem(ctx, desired)

		assert.ErrorIs(t, err, domain.ErrItemNotFound,
			"knowing another tenant's item id must not be enough to modify it")
		assert.Nil(t, updated)
	})
}

func updateItemRandomUUID(t *testing.T) pgtype.UUID {
	t.Helper()

	id := pgtype.UUID{Valid: true}
	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	return id
}
