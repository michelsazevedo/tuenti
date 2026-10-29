package persistence

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

var _ domain.CustomerRepository = (*CustomerRepository)(nil)

func TestCustomerRepositoryCreate(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)

	ctx := context.Background()

	t.Run("persists a customer without an address", func(t *testing.T) {
		customer := newTestCustomer(t, org)

		require.NoError(t, repo.Create(ctx, customer))

		assert.True(t, customer.ID.Valid, "the write must hand back a usable id")
		assert.False(t, customer.CreatedAt.IsZero(), "created_at must be hydrated from the database")
		assert.False(t, customer.UpdatedAt.IsZero(), "updated_at must be hydrated from the database")

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		assert.Nil(t, found.Address, "an all-NULL address must read back as nil")
	})

	t.Run("persists a customer with an address", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		customer.Address = fullTestAddress()

		require.NoError(t, repo.Create(ctx, customer))

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		require.NotNil(t, found.Address)
		assert.Equal(t, *fullTestAddress(), *found.Address)
	})

	t.Run("reconstructs a partial address with empty defaults", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		customer.Address = &domain.Address{City: "Austin"}

		require.NoError(t, repo.Create(ctx, customer))

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		require.NotNil(t, found.Address)
		assert.Equal(t, domain.Address{City: "Austin"}, *found.Address)
	})

	t.Run("reads an all-empty address back as nil", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		customer.Address = &domain.Address{}

		require.NoError(t, repo.Create(ctx, customer))

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		assert.Nil(t, found.Address)
	})

	t.Run("keeps a caller-supplied id", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		customer.ID = randomUUID(t)
		expected := customer.ID

		require.NoError(t, repo.Create(ctx, customer))

		assert.Equal(t, expected, customer.ID)
	})
}

func TestCustomerRepositoryFindByID(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)
	otherOrg := createTestOrganization(t, pool)

	ctx := context.Background()

	customer := newTestCustomer(t, org)
	customer.Address = fullTestAddress()
	require.NoError(t, repo.Create(ctx, customer))

	t.Run("round-trips every field", func(t *testing.T) {
		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)

		assert.Equal(t, customer.ID, found.ID)
		assert.Equal(t, org, found.OrganizationID)
		assert.Equal(t, customer.Name, found.Name)
		assert.Equal(t, customer.CompanyName, found.CompanyName)
		assert.Equal(t, customer.Email, found.Email)
		assert.Equal(t, customer.PhoneNumber, found.PhoneNumber)
		assert.Equal(t, customer.Address, found.Address)
		assert.True(t, customer.CreatedAt.Equal(found.CreatedAt))
		assert.True(t, customer.UpdatedAt.Equal(found.UpdatedAt))
		assert.Nil(t, found.DeletedAt)
	})

	t.Run("maps a missing row to ErrCustomerNotFound", func(t *testing.T) {
		found, err := repo.FindByID(ctx, org, randomUUID(t))

		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrCustomerNotFound)
	})

	t.Run("does not leak a customer across organizations", func(t *testing.T) {
		found, err := repo.FindByID(ctx, otherOrg, customer.ID)

		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrCustomerNotFound,
			"a valid id from another tenant must be indistinguishable from a missing row")
	})
}

func TestCustomerRepositoryUpdate(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)
	otherOrg := createTestOrganization(t, pool)

	ctx := context.Background()

	t.Run("persists the mutable columns and bumps updated_at", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, customer))

		createdAt, before := customer.CreatedAt, customer.UpdatedAt

		customer.Name = "Renamed " + randomSuffix(t)
		customer.CompanyName = "Road Runner Inc"
		customer.Email = "renamed." + randomSuffix(t) + "@example.com"
		customer.PhoneNumber = "+1 555 0199"

		require.NoError(t, repo.Update(ctx, customer))

		assert.True(t, createdAt.Equal(customer.CreatedAt), "created_at must be immutable")
		assert.False(t, customer.UpdatedAt.Before(before), "updated_at must not move backwards")

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		assert.Equal(t, customer.Name, found.Name)
		assert.Equal(t, customer.CompanyName, found.CompanyName)
		assert.Equal(t, customer.Email, found.Email)
		assert.Equal(t, customer.PhoneNumber, found.PhoneNumber)
	})

	t.Run("clears an existing address when set to nil", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		customer.Address = fullTestAddress()
		require.NoError(t, repo.Create(ctx, customer))

		customer.Address = nil
		require.NoError(t, repo.Update(ctx, customer))

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		assert.Nil(t, found.Address)
	})

	t.Run("adds an address to a customer that had none", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, customer))

		customer.Address = fullTestAddress()
		require.NoError(t, repo.Update(ctx, customer))

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		require.NotNil(t, found.Address)
		assert.Equal(t, *fullTestAddress(), *found.Address)
	})

	t.Run("maps a missing row to ErrCustomerNotFound", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		customer.ID = randomUUID(t)

		assert.ErrorIs(t, repo.Update(ctx, customer), domain.ErrCustomerNotFound)
	})

	t.Run("refuses to update a soft-deleted customer", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, customer))
		require.NoError(t, repo.Delete(ctx, org, customer.ID))

		assert.ErrorIs(t, repo.Update(ctx, customer), domain.ErrCustomerNotFound)
	})

	t.Run("refuses to update a customer from another organization", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, customer))
		originalName := customer.Name

		hijack := *customer
		hijack.OrganizationID = otherOrg
		hijack.Name = "Hijacked"

		assert.ErrorIs(t, repo.Update(ctx, &hijack), domain.ErrCustomerNotFound)

		found, err := repo.FindByID(ctx, org, customer.ID)
		require.NoError(t, err)
		assert.Equal(t, originalName, found.Name, "a cross-tenant update must not mutate the row")
	})
}

func TestCustomerRepositoryDelete(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)
	otherOrg := createTestOrganization(t, pool)

	ctx := context.Background()

	t.Run("soft-deletes and hides the customer", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, customer))

		require.NoError(t, repo.Delete(ctx, org, customer.ID))

		found, err := repo.FindByID(ctx, org, customer.ID)
		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrCustomerNotFound)

		var deleted bool

		err = pool.QueryRow(ctx,
			`SELECT deleted_at IS NOT NULL FROM customers WHERE id = $1`, customer.ID,
		).Scan(&deleted)
		require.NoError(t, err, "a soft delete must keep the row")
		assert.True(t, deleted, "deleted_at must be stamped")
	})

	t.Run("rejects deleting an already-deleted customer", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, customer))
		require.NoError(t, repo.Delete(ctx, org, customer.ID))

		assert.ErrorIs(t, repo.Delete(ctx, org, customer.ID), domain.ErrCustomerNotFound)
	})

	t.Run("maps a missing row to ErrCustomerNotFound", func(t *testing.T) {
		assert.ErrorIs(t, repo.Delete(ctx, org, randomUUID(t)), domain.ErrCustomerNotFound)
	})

	t.Run("refuses to delete a customer from another organization", func(t *testing.T) {
		customer := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, customer))

		assert.ErrorIs(t, repo.Delete(ctx, otherOrg, customer.ID), domain.ErrCustomerNotFound)

		_, err := repo.FindByID(ctx, org, customer.ID)
		assert.NoError(t, err, "a cross-tenant delete must leave the row active")
	})
}

func TestCustomerRepositoryList(t *testing.T) {
	repo, pool := newTestRepository(t)

	ctx := context.Background()

	t.Run("returns an empty slice for an organization without customers", func(t *testing.T) {
		org := createTestOrganization(t, pool)

		customers, err := repo.List(ctx, domain.ListCustomersFilter{OrganizationID: org})
		require.NoError(t, err)

		assert.NotNil(t, customers, "callers must be able to range over results without a nil check")
		assert.Empty(t, customers)
	})

	org := createTestOrganization(t, pool)
	otherOrg := createTestOrganization(t, pool)

	first := newTestCustomer(t, org)
	first.Name = "Bugs Bunny"
	first.CompanyName = "Carrot Holdings"
	first.Email = "bugs." + randomSuffix(t) + "@warner.test"
	first.Address = fullTestAddress()
	require.NoError(t, repo.Create(ctx, first))

	second := newTestCustomer(t, org)
	second.Name = "Daffy Duck"
	second.CompanyName = "Pond Ventures"
	second.Email = "daffy." + randomSuffix(t) + "@quack.test"
	require.NoError(t, repo.Create(ctx, second))

	third := newTestCustomer(t, org)
	third.Name = "Porky Pig"
	third.CompanyName = "Thats All Folks LLC"
	third.Email = "porky." + randomSuffix(t) + "@oink.test"
	require.NoError(t, repo.Create(ctx, third))

	deleted := newTestCustomer(t, org)
	deleted.Name = "Bugs Deleted"
	require.NoError(t, repo.Create(ctx, deleted))
	require.NoError(t, repo.Delete(ctx, org, deleted.ID))

	foreign := newTestCustomer(t, otherOrg)
	foreign.Name = "Bugs Foreign"
	require.NoError(t, repo.Create(ctx, foreign))

	t.Run("returns active customers of the organization, newest first", func(t *testing.T) {
		customers, err := repo.List(ctx, domain.ListCustomersFilter{OrganizationID: org})
		require.NoError(t, err)

		assert.Equal(t, []pgtype.UUID{third.ID, second.ID, first.ID}, customerIDs(customers),
			"deleted and foreign customers must be excluded; order is created_at DESC")

		require.NotNil(t, customers[2].Address, "addresses must be reconstructed on list too")
		assert.Equal(t, *fullTestAddress(), *customers[2].Address)
		assert.Nil(t, customers[1].Address)
	})

	tests := []struct {
		name   string
		search string
		want   []pgtype.UUID
	}{
		{name: "matches name case-insensitively", search: "daffy", want: []pgtype.UUID{second.ID}},
		{name: "matches company name", search: "carrot", want: []pgtype.UUID{first.ID}},
		{name: "matches email", search: "@OINK.test", want: []pgtype.UUID{third.ID}},
		{name: "matches a substring across customers", search: "u", want: []pgtype.UUID{second.ID, first.ID}},
		{name: "trims surrounding whitespace", search: "  Porky  ", want: []pgtype.UUID{third.ID}},
		{name: "ignores a blank search", search: "   ", want: []pgtype.UUID{third.ID, second.ID, first.ID}},
		{name: "excludes deleted and foreign matches", search: "bugs", want: []pgtype.UUID{first.ID}},
		{name: "returns nothing when no customer matches", search: "elmer", want: []pgtype.UUID{}},
	}

	for _, tt := range tests {
		t.Run("search "+tt.name, func(t *testing.T) {
			customers, err := repo.List(ctx, domain.ListCustomersFilter{OrganizationID: org, Search: tt.search})
			require.NoError(t, err)

			assert.Equal(t, tt.want, customerIDs(customers))
		})
	}
}

func TestCustomerRepositoryDuplicateEmail(t *testing.T) {
	repo, pool := newTestRepository(t)
	org := createTestOrganization(t, pool)
	otherOrg := createTestOrganization(t, pool)

	ctx := context.Background()

	existing := newTestCustomer(t, org)
	require.NoError(t, repo.Create(ctx, existing))

	t.Run("rejects a duplicate email on create", func(t *testing.T) {
		duplicate := newTestCustomer(t, org)
		duplicate.Email = existing.Email

		assert.ErrorIs(t, repo.Create(ctx, duplicate), domain.ErrCustomerEmailTaken)
	})

	t.Run("rejects a duplicate email differing only in case", func(t *testing.T) {
		duplicate := newTestCustomer(t, org)
		duplicate.Email = strings.ToUpper(existing.Email)

		assert.ErrorIs(t, repo.Create(ctx, duplicate), domain.ErrCustomerEmailTaken)
	})

	t.Run("rejects a duplicate email on update", func(t *testing.T) {
		other := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, other))

		other.Email = existing.Email

		assert.ErrorIs(t, repo.Update(ctx, other), domain.ErrCustomerEmailTaken)
	})

	t.Run("allows the same email in a different organization", func(t *testing.T) {
		foreign := newTestCustomer(t, otherOrg)
		foreign.Email = existing.Email

		assert.NoError(t, repo.Create(ctx, foreign))
	})

	t.Run("allows reusing the email of a soft-deleted customer", func(t *testing.T) {
		gone := newTestCustomer(t, org)
		require.NoError(t, repo.Create(ctx, gone))
		require.NoError(t, repo.Delete(ctx, org, gone.ID))

		replacement := newTestCustomer(t, org)
		replacement.Email = gone.Email

		assert.NoError(t, repo.Create(ctx, replacement))
	})
}
