package application

import (
	"context"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type UpdateCustomer struct {
	repo domain.CustomerRepository
}

func NewUpdateCustomer(repo domain.CustomerRepository) *UpdateCustomer {
	return &UpdateCustomer{repo: repo}
}

func (uc *UpdateCustomer) UpdateCustomer(ctx context.Context, customer *domain.Customer) (*domain.Customer, error) {
	if err := customer.Validate(); err != nil {
		return nil, err
	}

	if err := uc.repo.Update(ctx, customer); err != nil {
		return nil, err
	}

	logCustomerUpdated(ctx, customer)

	return customer, nil
}

func logCustomerUpdated(ctx context.Context, customer *domain.Customer) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", "customer_updated").
		Str("customer_id", customer.ID.String()).
		Str("organization_id", customer.OrganizationID.String()).
		Msg("Customer updated")
}
