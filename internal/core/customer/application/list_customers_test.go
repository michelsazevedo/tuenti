package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

type listSpyCustomerRepository struct {
	customers []*domain.Customer
	listErr   error
	listCalls int
	filter    domain.ListCustomersFilter
}

func (r *listSpyCustomerRepository) Create(ctx context.Context, customer *domain.Customer) error {
	return errors.New("unexpected call to Create")
}

func (r *listSpyCustomerRepository) Update(ctx context.Context, customer *domain.Customer) error {
	return errors.New("unexpected call to Update")
}

func (r *listSpyCustomerRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	return errors.New("unexpected call to Delete")
}

func (r *listSpyCustomerRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Customer, error) {
	return nil, errors.New("unexpected call to FindByID")
}

func (r *listSpyCustomerRepository) List(ctx context.Context, filter domain.ListCustomersFilter) ([]*domain.Customer, error) {
	r.listCalls++
	r.filter = filter

	if r.listErr != nil {
		return nil, r.listErr
	}

	return r.customers, nil
}

func TestListCustomers(t *testing.T) {
	customers := []*domain.Customer{
		{ID: customerTestID, OrganizationID: customerTestOrganizationID, Name: "Jane Doe"},
		{OrganizationID: customerTestOrganizationID, Name: "John Roe"},
	}
	infraErr := errors.New("postgres: connection refused")

	tests := []struct {
		name      string
		filter    domain.ListCustomersFilter
		customers []*domain.Customer
		listErr   error
		want      []*domain.Customer
		wantErr   error
	}{
		{
			name:      "returns the customers unchanged",
			filter:    domain.ListCustomersFilter{OrganizationID: customerTestOrganizationID},
			customers: customers,
			want:      customers,
		},
		{
			name:      "forwards the search term untouched",
			filter:    domain.ListCustomersFilter{OrganizationID: customerTestOrganizationID, Search: "acme"},
			customers: customers[:1],
			want:      customers[:1],
		},
		{
			name:      "returns an empty result as-is",
			filter:    domain.ListCustomersFilter{OrganizationID: customerTestOrganizationID},
			customers: []*domain.Customer{},
			want:      []*domain.Customer{},
		},
		{
			name:    "infrastructure failure propagates unchanged",
			filter:  domain.ListCustomersFilter{OrganizationID: customerTestOrganizationID},
			listErr: infraErr,
			wantErr: infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &listSpyCustomerRepository{customers: tt.customers, listErr: tt.listErr}

			got, err := NewListCustomers(repo).ListCustomers(context.Background(), tt.filter)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.Equal(t, 1, repo.listCalls)
			assert.Equal(t, tt.filter, repo.filter, "the filter, including its organization scope, must reach the repository intact")
		})
	}
}
