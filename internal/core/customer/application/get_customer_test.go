package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

type findSpyCustomerRepository struct {
	customer           *domain.Customer
	findErr            error
	findCalls          int
	findOrganizationID pgtype.UUID
	findID             pgtype.UUID
}

func (r *findSpyCustomerRepository) Create(ctx context.Context, customer *domain.Customer) error {
	return errors.New("unexpected call to Create")
}

func (r *findSpyCustomerRepository) Update(ctx context.Context, customer *domain.Customer) error {
	return errors.New("unexpected call to Update")
}

func (r *findSpyCustomerRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	return errors.New("unexpected call to Delete")
}

func (r *findSpyCustomerRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Customer, error) {
	r.findCalls++
	r.findOrganizationID = organizationID
	r.findID = id

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.customer, nil
}

func (r *findSpyCustomerRepository) List(ctx context.Context, filter domain.ListCustomersFilter) ([]*domain.Customer, error) {
	return nil, errors.New("unexpected call to List")
}

func TestGetCustomer(t *testing.T) {
	customer := &domain.Customer{
		ID:             customerTestID,
		OrganizationID: customerTestOrganizationID,
		Name:           "Jane Doe",
		CompanyName:    "Acme Corp",
		Email:          "jane@example.com",
		PhoneNumber:    "+1 555 0100",
		CreatedAt:      time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
	}
	infraErr := errors.New("postgres: connection refused")

	tests := []struct {
		name     string
		customer *domain.Customer
		findErr  error
		want     *domain.Customer
		wantErr  error
	}{
		{
			name:     "returns the customer unchanged",
			customer: customer,
			want:     customer,
		},
		{
			name:    "propagates not found unchanged",
			findErr: domain.ErrCustomerNotFound,
			wantErr: domain.ErrCustomerNotFound,
		},
		{
			name:    "infrastructure failure propagates unchanged",
			findErr: infraErr,
			wantErr: infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customers := &findSpyCustomerRepository{customer: tt.customer, findErr: tt.findErr}

			got, err := NewGetCustomer(customers).GetCustomer(context.Background(), customerTestOrganizationID, customerTestID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.Same(t, tt.want, got)
			}

			assert.Equal(t, 1, customers.findCalls)
			assert.Equal(t, customerTestOrganizationID, customers.findOrganizationID,
				"the lookup must be scoped to the caller's organization")
			assert.Equal(t, customerTestID, customers.findID)
		})
	}
}
