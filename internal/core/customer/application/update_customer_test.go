package application

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/core/customer/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

func newUpdateCustomerUseCase(pgConn *database.PgConn) *UpdateCustomer {
	return NewUpdateCustomer(database.NewUnitOfWork(pgConn))
}

func updateCustomerFixture(t *testing.T, pgConn *database.PgConn, organizationID pgtype.UUID) *domain.Customer {
	t.Helper()

	customer := newCustomerInput(t, organizationID)
	require.NoError(t, customer.Validate())
	require.NoError(t, persistence.NewCustomerRepository(pgConn.Pool()).Create(customerTestContext(t), customer))

	return customer
}

func TestUpdateCustomerRejectsInvalidAggregatesWithoutTouchingPersistence(t *testing.T) {
	for _, tt := range customerValidationCases {
		t.Run(tt.name, func(t *testing.T) {
			input := newCustomerInput(t, customerTestOrganizationID)
			input.ID = customerTestID
			tt.mutate(input)

			updated, err := NewUpdateCustomer(newUnreachableUnitOfWork()).UpdateCustomer(customerTestContext(t), input)

			require.ErrorIs(t, err, tt.wantErr, "the use case must surface the violated domain rule verbatim")
			assert.Nil(t, updated)
		})
	}
}

func TestUpdateCustomerPersistsChanges(t *testing.T) {
	pgConn := newCustomerTestPgConn(t)
	ctx := customerTestContext(t)

	organizationID := customerTestOrganization(t, pgConn.Pool())
	existing := updateCustomerFixture(t, pgConn, organizationID)

	desired := newCustomerInput(t, organizationID)
	desired.ID = existing.ID
	desired.CompanyName = "Globex"
	desired.Address = nil

	updated, err := newUpdateCustomerUseCase(pgConn).UpdateCustomer(ctx, desired)

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, existing.ID, updated.ID)

	found, err := persistence.NewCustomerRepository(pgConn.Pool()).FindByID(ctx, organizationID, existing.ID)
	require.NoError(t, err)

	assert.Equal(t, desired.Name, found.Name)
	assert.Equal(t, "Globex", found.CompanyName)
	assert.Equal(t, desired.Email, found.Email)
}

func TestUpdateCustomerReturnsNotFoundForMissingOrCrossTenantCustomers(t *testing.T) {
	pgConn := newCustomerTestPgConn(t)
	ctx := customerTestContext(t)

	organizationID := customerTestOrganization(t, pgConn.Pool())
	otherOrganizationID := customerTestOrganization(t, pgConn.Pool())
	existing := updateCustomerFixture(t, pgConn, organizationID)

	t.Run("an id that does not exist", func(t *testing.T) {
		desired := newCustomerInput(t, organizationID)
		desired.ID = customerRandomUUID(t)

		updated, err := newUpdateCustomerUseCase(pgConn).UpdateCustomer(ctx, desired)

		assert.ErrorIs(t, err, domain.ErrCustomerNotFound)
		assert.Nil(t, updated)
	})

	t.Run("a customer owned by another organization", func(t *testing.T) {
		desired := newCustomerInput(t, otherOrganizationID)
		desired.ID = existing.ID

		updated, err := newUpdateCustomerUseCase(pgConn).UpdateCustomer(ctx, desired)

		assert.ErrorIs(t, err, domain.ErrCustomerNotFound,
			"knowing another tenant's customer id must not be enough to modify it")
		assert.Nil(t, updated)
	})
}

// TestUpdateCustomerEmitsIdentifiersOnly swaps the global logger through
// captureLogs, so it must not run in parallel.
func TestUpdateCustomerEmitsIdentifiersOnly(t *testing.T) {
	pgConn := newCustomerTestPgConn(t)
	organizationID := customerTestOrganization(t, pgConn.Pool())
	existing := updateCustomerFixture(t, pgConn, organizationID)

	desired := newCustomerInput(t, organizationID)
	desired.ID = existing.ID

	logs := captureLogs(t)

	_, err := newUpdateCustomerUseCase(pgConn).UpdateCustomer(customerTestContext(t), desired)
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry), logs.String())

	assert.Equal(t, map[string]any{
		"level":           "info",
		"event":           "customer_updated",
		"customer_id":     existing.ID.String(),
		"organization_id": organizationID.String(),
		"message":         "Customer updated",
	}, entry, "customer events must carry IDs only: any extra field risks leaking PII")
}

func TestUpdateCustomerDoesNotLogAMissingCustomer(t *testing.T) {
	pgConn := newCustomerTestPgConn(t)
	organizationID := customerTestOrganization(t, pgConn.Pool())

	desired := newCustomerInput(t, organizationID)
	desired.ID = customerRandomUUID(t)

	logs := captureLogs(t)

	_, err := newUpdateCustomerUseCase(pgConn).UpdateCustomer(customerTestContext(t), desired)
	require.ErrorIs(t, err, domain.ErrCustomerNotFound)

	assert.Empty(t, logs.String(), "a failed update must not be reported as one that happened")
}
