package application

import (
	"context"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type CreateCustomerUseCase interface {
	CreateCustomer(ctx context.Context, customer *domain.Customer) (*domain.Customer, error)
}

type createCustomer struct {
	customers domain.CustomerRepository
}

func NewCreateCustomer(customers domain.CustomerRepository) CreateCustomerUseCase {
	return &createCustomer{customers: customers}
}

func (c *createCustomer) CreateCustomer(ctx context.Context, customer *domain.Customer) (*domain.Customer, error) {
	if err := customer.Validate(); err != nil {
		return nil, err
	}

	if err := c.customers.Create(ctx, customer); err != nil {
		return nil, err
	}

	logCustomerCreated(ctx, customer)

	return customer, nil
}

func logCustomerCreated(ctx context.Context, customer *domain.Customer) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", "customer_created").
		Str("customer_id", customer.ID.String()).
		Str("organization_id", customer.OrganizationID.String()).
		Msg("Customer created")
}
