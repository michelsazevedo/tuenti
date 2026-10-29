package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

type deleteSpyCustomerRepository struct {
	deleteErr            error
	deleteCalls          int
	deleteOrganizationID pgtype.UUID
	deleteID             pgtype.UUID
}

func (r *deleteSpyCustomerRepository) Create(ctx context.Context, customer *domain.Customer) error {
	return errors.New("unexpected call to Create")
}

func (r *deleteSpyCustomerRepository) Update(ctx context.Context, customer *domain.Customer) error {
	return errors.New("unexpected call to Update")
}

func (r *deleteSpyCustomerRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	r.deleteCalls++
	r.deleteOrganizationID = organizationID
	r.deleteID = id

	return r.deleteErr
}

func (r *deleteSpyCustomerRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Customer, error) {
	return nil, errors.New("unexpected call to FindByID")
}

func (r *deleteSpyCustomerRepository) List(ctx context.Context, filter domain.ListCustomersFilter) ([]*domain.Customer, error) {
	return nil, errors.New("unexpected call to List")
}

// captureLogs redirects the global logger observability.Logger reads from.
// It mutates package-level state, so callers must not run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer

	previous := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = previous })

	return &buf
}

func TestDeleteCustomer(t *testing.T) {
	infraErr := errors.New("postgres: connection refused")

	tests := []struct {
		name      string
		deleteErr error
		wantErr   error
	}{
		{
			name: "deletes successfully",
		},
		{
			name:      "propagates not found unchanged",
			deleteErr: domain.ErrCustomerNotFound,
			wantErr:   domain.ErrCustomerNotFound,
		},
		{
			name:      "infrastructure failure propagates unchanged",
			deleteErr: infraErr,
			wantErr:   infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			customers := &deleteSpyCustomerRepository{deleteErr: tt.deleteErr}

			err := NewDeleteCustomer(customers).DeleteCustomer(context.Background(), customerTestOrganizationID, customerTestID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, logs.String(), "a failed delete must not be reported as one that happened")
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, logs.String(), "a successful delete must emit customer_deleted")
			}

			assert.Equal(t, 1, customers.deleteCalls)
			assert.Equal(t, customerTestOrganizationID, customers.deleteOrganizationID)
			assert.Equal(t, customerTestID, customers.deleteID)
		})
	}
}

func TestLogCustomerEventEmitsIdentifiersOnly(t *testing.T) {
	logs := captureLogs(t)

	err := NewDeleteCustomer(&deleteSpyCustomerRepository{}).
		DeleteCustomer(context.Background(), customerTestOrganizationID, customerTestID)
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))

	assert.Equal(t, map[string]any{
		"level":           "info",
		"event":           "customer_deleted",
		"customer_id":     customerTestID.String(),
		"organization_id": customerTestOrganizationID.String(),
		"message":         "Customer deleted",
	}, entry, "customer events must carry IDs only: any extra field risks leaking PII")
}
