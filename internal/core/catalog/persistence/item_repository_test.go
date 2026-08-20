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
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

var _ domain.ItemRepository = (*ItemRepository)(nil)

const testTimeout = 5 * time.Second

func TestItemRepositoryCreate(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)

	ctx := context.Background()

	t.Run("persists the item and its taxes", func(t *testing.T) {
		item := newTestItem(t, org)
		item.Taxes = []domain.ItemTax{
			{Name: "VAT", TaxNumber: "VAT-001", Rate: decimal.RequireFromString("7.25"), Enabled: true},
			{Name: "City", TaxNumber: "CITY-002", Rate: decimal.RequireFromString("1.50"), Enabled: false},
		}

		require.NoError(t, repo.Create(ctx, item))

		assert.True(t, item.ID.Valid, "the write must hand back a usable id")
		assert.False(t, item.CreatedAt.IsZero(), "created_at must be hydrated from the database")
		assert.False(t, item.UpdatedAt.IsZero(), "updated_at must be hydrated from the database")

		for _, tax := range item.Taxes {
			assert.True(t, tax.ID.Valid, "every child tax must come back with an id")
			assert.Equal(t, item.ID, tax.ItemID, "taxes must be wired to their parent item")
			assert.False(t, tax.CreatedAt.IsZero())
		}
	})

	t.Run("keeps a caller-supplied id", func(t *testing.T) {
		item := newTestItem(t, org)
		item.ID = randomUUID(t)
		expected := item.ID

		require.NoError(t, repo.Create(ctx, item))

		assert.Equal(t, expected, item.ID,
			"an id minted by the application layer must survive the insert unchanged")
	})

	t.Run("generates an id when the caller supplied none", func(t *testing.T) {
		item := newTestItem(t, org)
		require.False(t, item.ID.Valid)

		require.NoError(t, repo.Create(ctx, item))

		assert.True(t, item.ID.Valid)
		assert.NotEqual(t, pgtype.UUID{}.Bytes, item.ID.Bytes, "the generated id must not be the zero UUID")
	})
}

func TestItemRepositoryFindByID(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)

	ctx := context.Background()

	t.Run("round-trips every field at full decimal precision", func(t *testing.T) {
		item := newTestItem(t, org)
		item.Rate = decimal.RequireFromString("1999.99")
		item.Description = "A very fine anvil"
		item.Taxes = []domain.ItemTax{
			{Name: "VAT", TaxNumber: "VAT-001", Rate: decimal.RequireFromString("7.25"), Enabled: true},
		}
		require.NoError(t, repo.Create(ctx, item))

		found, err := repo.FindByID(ctx, org, item.ID)
		require.NoError(t, err)

		assert.Equal(t, item.ID, found.ID)
		assert.Equal(t, item.Name, found.Name)
		assert.Equal(t, item.Description, found.Description)
		assert.Equal(t, domain.ItemTypeItem, found.Type)
		assert.Equal(t, item.Currency, found.Currency)
		assert.Equal(t, item.IncomeAccount, found.IncomeAccount)
		assert.True(t, found.TrackInventory)
		assert.Equal(t, item.QuantityInStock, found.QuantityInStock)
		assert.Nil(t, found.DeletedAt)

		assert.Equal(t, "1999.99", found.Rate.String(),
			"money must survive NUMERIC without drifting through a float")

		require.Len(t, found.Taxes, 1)
		assert.Equal(t, "VAT", found.Taxes[0].Name)
		assert.Equal(t, "VAT-001", found.Taxes[0].TaxNumber)
		assert.True(t, found.Taxes[0].Enabled)
		assert.Equal(t, "7.25", found.Taxes[0].Rate.String(),
			"a tax rate must keep its exact scale across the round trip")
	})

	t.Run("returns an empty tax slice when the item has none", func(t *testing.T) {
		item := newTestItem(t, org)
		require.NoError(t, repo.Create(ctx, item))

		found, err := repo.FindByID(ctx, org, item.ID)
		require.NoError(t, err)

		assert.NotNil(t, found.Taxes, "callers must be able to range over taxes without a nil check")
		assert.Empty(t, found.Taxes)
	})

	t.Run("maps a missing row to ErrItemNotFound", func(t *testing.T) {
		found, err := repo.FindByID(ctx, org, randomUUID(t))

		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrItemNotFound, "an absent row is a domain error, not a driver error")
	})
}

func TestItemRepositoryUpdate(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)

	ctx := context.Background()

	t.Run("persists the mutable columns and bumps updated_at", func(t *testing.T) {
		item := newTestItem(t, org)
		require.NoError(t, repo.Create(ctx, item))

		before := item.UpdatedAt

		item.Name = "Renamed " + randomSuffix(t)
		item.Description = "Now with rockets"
		item.Rate = decimal.RequireFromString("2500.50")
		item.Currency = "EUR"
		item.IncomeAccount = "4010"
		item.QuantityInStock = 42

		require.NoError(t, repo.Update(ctx, item))

		found, err := repo.FindByID(ctx, org, item.ID)
		require.NoError(t, err)

		assert.Equal(t, item.Name, found.Name)
		assert.Equal(t, "Now with rockets", found.Description)
		assert.Equal(t, "2500.5", found.Rate.String())
		assert.Equal(t, "EUR", found.Currency)
		assert.Equal(t, "4010", found.IncomeAccount)
		assert.Equal(t, 42, found.QuantityInStock)
		assert.True(t, found.UpdatedAt.After(before),
			"the write must refresh updated_at, otherwise the row looks untouched")
	})

	t.Run("replaces the tax list wholesale", func(t *testing.T) {
		item := newTestItem(t, org)
		item.Taxes = []domain.ItemTax{
			{Name: "Old A", Rate: decimal.RequireFromString("5.00"), Enabled: true},
			{Name: "Old B", Rate: decimal.RequireFromString("6.00"), Enabled: true},
		}
		require.NoError(t, repo.Create(ctx, item))

		item.Taxes = []domain.ItemTax{
			{Name: "New Only", TaxNumber: "N-1", Rate: decimal.RequireFromString("9.99"), Enabled: false},
		}
		require.NoError(t, repo.Update(ctx, item))

		found, err := repo.FindByID(ctx, org, item.ID)
		require.NoError(t, err)

		require.Len(t, found.Taxes, 1, "taxes dropped from the aggregate must not survive the write")
		assert.Equal(t, "New Only", found.Taxes[0].Name)
		assert.Equal(t, "9.99", found.Taxes[0].Rate.String())
		assert.False(t, found.Taxes[0].Enabled)
	})

	t.Run("clears the tax list when the aggregate has none", func(t *testing.T) {
		item := newTestItem(t, org)
		item.Taxes = []domain.ItemTax{{Name: "VAT", Rate: decimal.RequireFromString("7.25"), Enabled: true}}
		require.NoError(t, repo.Create(ctx, item))

		item.Taxes = nil
		require.NoError(t, repo.Update(ctx, item))

		found, err := repo.FindByID(ctx, org, item.ID)
		require.NoError(t, err)
		assert.Empty(t, found.Taxes)
	})

	t.Run("preserves a tax id the caller supplied", func(t *testing.T) {
		item := newTestItem(t, org)
		item.Taxes = []domain.ItemTax{{Name: "VAT", Rate: decimal.RequireFromString("7.25"), Enabled: true}}
		require.NoError(t, repo.Create(ctx, item))

		kept := item.Taxes[0].ID
		item.Taxes[0].Name = "VAT (revised)"
		require.NoError(t, repo.Update(ctx, item))

		found, err := repo.FindByID(ctx, org, item.ID)
		require.NoError(t, err)

		require.Len(t, found.Taxes, 1)
		assert.Equal(t, kept, found.Taxes[0].ID,
			"a tax round-tripped through the aggregate keeps its identity")
		assert.Equal(t, "VAT (revised)", found.Taxes[0].Name)
	})

	t.Run("maps a missing row to ErrItemNotFound", func(t *testing.T) {
		item := newTestItem(t, org)
		item.ID = randomUUID(t)

		assert.ErrorIs(t, repo.Update(ctx, item), domain.ErrItemNotFound)
	})

	t.Run("refuses to update an item owned by another organization", func(t *testing.T) {
		other := createTestOrganization(t, pool)

		item := newTestItem(t, org)
		require.NoError(t, repo.Create(ctx, item))

		hijacked := *item
		hijacked.OrganizationID = other
		hijacked.Name = "Hijacked"

		assert.ErrorIs(t, repo.Update(ctx, &hijacked), domain.ErrItemNotFound,
			"the tenant scope belongs in the WHERE clause, not only in the caller")

		found, err := repo.FindByID(ctx, org, item.ID)
		require.NoError(t, err)
		assert.Equal(t, item.Name, found.Name, "the rejected update must not have touched the row")
	})
}

func TestItemRepositoryDelete(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)

	ctx := context.Background()

	t.Run("soft-deletes and hides the item from reads", func(t *testing.T) {
		item := newTestItem(t, org)
		require.NoError(t, repo.Create(ctx, item))

		require.NoError(t, repo.Delete(ctx, org, item.ID))

		found, err := repo.FindByID(ctx, org, item.ID)
		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrItemNotFound)

		listed, err := repo.List(ctx, domain.ListItemsFilter{OrganizationID: org})
		require.NoError(t, err)
		assert.NotContains(t, itemIDs(listed), item.ID, "a deleted item must drop out of listings")

		assert.True(t, rowExists(t, pool, `SELECT 1 FROM items WHERE id = $1`, item.ID),
			"delete must be a soft delete, so the row itself survives")
	})

	t.Run("maps a missing row to ErrItemNotFound", func(t *testing.T) {
		assert.ErrorIs(t, repo.Delete(ctx, org, randomUUID(t)), domain.ErrItemNotFound)
	})

	t.Run("is not repeatable", func(t *testing.T) {
		item := newTestItem(t, org)
		require.NoError(t, repo.Create(ctx, item))
		require.NoError(t, repo.Delete(ctx, org, item.ID))

		assert.ErrorIs(t, repo.Delete(ctx, org, item.ID), domain.ErrItemNotFound,
			"deleting an already deleted item must not report success")
	})

	t.Run("refuses to delete across organizations", func(t *testing.T) {
		other := createTestOrganization(t, pool)

		item := newTestItem(t, org)
		require.NoError(t, repo.Create(ctx, item))

		assert.ErrorIs(t, repo.Delete(ctx, other, item.ID), domain.ErrItemNotFound)

		_, err := repo.FindByID(ctx, org, item.ID)
		assert.NoError(t, err, "the item must still belong to its owner")
	})
}

func TestItemRepositoryList(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)

	ctx := context.Background()

	anvil := newTestItem(t, org)
	anvil.Name = "Acme Anvil"
	anvil.Type = domain.ItemTypeItem
	anvil.Taxes = []domain.ItemTax{{Name: "VAT", Rate: decimal.RequireFromString("7.25"), Enabled: true}}
	require.NoError(t, repo.Create(ctx, anvil))

	rocket := newTestItem(t, org)
	rocket.Name = "Acme Rocket Skates"
	rocket.Type = domain.ItemTypeItem
	require.NoError(t, repo.Create(ctx, rocket))

	consulting := newTestItem(t, org)
	consulting.Name = "Rocket Consulting"
	consulting.Type = domain.ItemTypeService
	consulting.TrackInventory = false
	consulting.QuantityInStock = 0
	require.NoError(t, repo.Create(ctx, consulting))

	service := domain.ItemTypeService
	itemType := domain.ItemTypeItem

	tests := []struct {
		name     string
		filter   domain.ListItemsFilter
		expected []pgtype.UUID
		excluded []pgtype.UUID
	}{
		{
			name:     "no filter returns every live item in the organization",
			filter:   domain.ListItemsFilter{OrganizationID: org},
			expected: []pgtype.UUID{anvil.ID, rocket.ID, consulting.ID},
		},
		{
			name:     "type filter keeps only matching items",
			filter:   domain.ListItemsFilter{OrganizationID: org, Type: &service},
			expected: []pgtype.UUID{consulting.ID},
			excluded: []pgtype.UUID{anvil.ID, rocket.ID},
		},
		{
			name:     "search matches a name fragment case-insensitively",
			filter:   domain.ListItemsFilter{OrganizationID: org, Search: "rocket"},
			expected: []pgtype.UUID{rocket.ID, consulting.ID},
			excluded: []pgtype.UUID{anvil.ID},
		},
		{
			name:     "type and search compose",
			filter:   domain.ListItemsFilter{OrganizationID: org, Type: &itemType, Search: "rocket"},
			expected: []pgtype.UUID{rocket.ID},
			excluded: []pgtype.UUID{anvil.ID, consulting.ID},
		},
		{
			name:     "a search that matches nothing yields nothing",
			filter:   domain.ListItemsFilter{OrganizationID: org, Search: "no-such-item-" + randomSuffix(t)},
			excluded: []pgtype.UUID{anvil.ID, rocket.ID, consulting.ID},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			found, err := repo.List(ctx, test.filter)
			require.NoError(t, err)

			ids := itemIDs(found)
			for _, id := range test.expected {
				assert.Contains(t, ids, id)
			}

			for _, id := range test.excluded {
				assert.NotContains(t, ids, id)
			}
		})
	}

	t.Run("hydrates the taxes of every listed item", func(t *testing.T) {
		found, err := repo.List(ctx, domain.ListItemsFilter{OrganizationID: org})
		require.NoError(t, err)

		listed := requireItemInResult(t, found, anvil.ID)
		require.Len(t, listed.Taxes, 1, "a listing must not return half-built aggregates")
		assert.Equal(t, "7.25", listed.Taxes[0].Rate.String())
		assert.Equal(t, anvil.ID, listed.Taxes[0].ItemID)

		untaxed := requireItemInResult(t, found, rocket.ID)
		assert.NotNil(t, untaxed.Taxes)
		assert.Empty(t, untaxed.Taxes, "taxes must not bleed between items in a batched fetch")
	})

	t.Run("returns an empty slice for an organization with no items", func(t *testing.T) {
		empty := createTestOrganization(t, pool)

		found, err := repo.List(ctx, domain.ListItemsFilter{OrganizationID: empty})

		require.NoError(t, err)
		assert.NotNil(t, found, "callers must be able to range over the result without a nil check")
		assert.Empty(t, found)
	})
}

func TestItemRepositoryTenantIsolation(t *testing.T) {
	repo, pool := newTestRepository(t)

	ctx := context.Background()

	tenant := createTestOrganization(t, pool)
	intruder := createTestOrganization(t, pool)

	item := newTestItem(t, tenant)
	require.NoError(t, repo.Create(ctx, item))

	t.Run("FindByID does not cross the organization boundary", func(t *testing.T) {
		found, err := repo.FindByID(ctx, intruder, item.ID)

		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrItemNotFound,
			"knowing an item id must not be enough to read another tenant's catalog")
	})

	t.Run("List does not cross the organization boundary", func(t *testing.T) {
		found, err := repo.List(ctx, domain.ListItemsFilter{OrganizationID: intruder})

		require.NoError(t, err)
		assert.NotContains(t, itemIDs(found), item.ID)
	})

}

func newTestRepository(t *testing.T) (*ItemRepository, *pgxpool.Pool) {
	t.Helper()

	conn := newTestConn(t)

	return NewItemRepository(conn.Pool()), conn.Pool()
}

func newTestConn(t *testing.T) *database.PgConn {
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

	return pgConn
}

func requireSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	for _, table := range []string{"organizations", "items", "item_taxes"} {
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

		if _, err := pool.Exec(ctx, `DELETE FROM items WHERE organization_id = $1`, id); err != nil {
			t.Errorf("cleanup failed for items: %v", err)
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
		"Catalog Test Industry "+suffix, "catalog_test_industry_"+suffix,
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

func newTestItem(t *testing.T, organizationID pgtype.UUID) *domain.Item {
	t.Helper()

	return &domain.Item{
		OrganizationID:  organizationID,
		Name:            "Acme Anvil " + randomSuffix(t),
		Description:     "Heavy",
		Type:            domain.ItemTypeItem,
		Rate:            decimal.RequireFromString("1999.99"),
		Currency:        "USD",
		IncomeAccount:   "4000",
		TrackInventory:  true,
		QuantityInStock: 10,
	}
}

func rowExists(t *testing.T, pool *pgxpool.Pool, sql string, id pgtype.UUID) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	var one int

	return pool.QueryRow(ctx, sql, id).Scan(&one) == nil
}

func itemIDs(items []*domain.Item) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}

	return ids
}

func requireItemInResult(t *testing.T, items []*domain.Item, id pgtype.UUID) *domain.Item {
	t.Helper()

	for _, item := range items {
		if item.ID == id {
			return item
		}
	}

	t.Fatalf("item %x is missing from the result set", id.Bytes)

	return nil
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
