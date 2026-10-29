package application

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/customer/persistence"
)

func TestCreateCustomerRejectsInvalidAggregatesWithoutTouchingPersistence(t *testing.T) {
	for _, tt := range customerValidationCases {
		t.Run(tt.name, func(t *testing.T) {
			input := newCustomerInput(t, customerTestOrganizationID)
			tt.mutate(input)

			created, err := NewCreateCustomer(newUnreachableCustomerRepository(t)).CreateCustomer(customerTestContext(t), input)

			require.ErrorIs(t, err, tt.wantErr, "the use case must surface the violated domain rule verbatim")
			assert.Nil(t, created, "a rejected aggregate must not be handed back as if it were created")
		})
	}
}

func TestCreateCustomerPersistsACustomer(t *testing.T) {
	pgConn := newCustomerTestPgConn(t)
	ctx := customerTestContext(t)

	organizationID := customerTestOrganization(t, pgConn.Pool())
	input := newCustomerInput(t, organizationID)

	created, err := NewCreateCustomer(persistence.NewCustomerRepository(pgConn.Pool())).CreateCustomer(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, created)

	assert.True(t, created.ID.Valid, "the insert must hand back a usable id")
	assert.False(t, created.CreatedAt.IsZero(), "created_at must be hydrated from the database")
	assert.Equal(t, "US", created.Address.Country, "validation must normalize the country before it is written")

	found, err := persistence.NewCustomerRepository(pgConn.Pool()).FindByID(ctx, organizationID, created.ID)
	require.NoError(t, err, "the created customer must be readable through the repository")

	assert.Equal(t, input.Name, found.Name)
	assert.Equal(t, input.Email, found.Email)
	require.NotNil(t, found.Address)
	assert.Equal(t, "US", found.Address.Country)
}

// TestCreateCustomerEmitsIdentifiersOnly swaps the global logger through
// captureLogs, so it must not run in parallel.
func TestCreateCustomerEmitsIdentifiersOnly(t *testing.T) {
	pgConn := newCustomerTestPgConn(t)
	organizationID := customerTestOrganization(t, pgConn.Pool())
	input := newCustomerInput(t, organizationID)

	logs := captureLogs(t)

	created, err := NewCreateCustomer(persistence.NewCustomerRepository(pgConn.Pool())).CreateCustomer(customerTestContext(t), input)
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry), logs.String())

	assert.Equal(t, map[string]any{
		"level":           "info",
		"event":           "customer_created",
		"customer_id":     created.ID.String(),
		"organization_id": organizationID.String(),
		"message":         "Customer created",
	}, entry, "customer events must carry IDs only: any extra field risks leaking PII")
}

func TestCreateCustomerDoesNotLogARejectedCustomer(t *testing.T) {
	logs := captureLogs(t)

	input := newCustomerInput(t, customerTestOrganizationID)
	input.Name = ""

	_, err := NewCreateCustomer(newUnreachableCustomerRepository(t)).CreateCustomer(customerTestContext(t), input)
	require.Error(t, err)

	assert.Empty(t, logs.String(), "a rejected create must not be reported as one that happened")
}
