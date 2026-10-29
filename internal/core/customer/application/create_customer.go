package application

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/core/customer/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

type CreateCustomerUseCase interface {
	CreateCustomer(ctx context.Context, customer *domain.Customer) (*domain.Customer, error)
}

type createCustomer struct {
	uow *database.UnitOfWork
}

func NewCreateCustomer(uow *database.UnitOfWork) CreateCustomerUseCase {
	return &createCustomer{uow: uow}
}

func (c *createCustomer) CreateCustomer(ctx context.Context, customer *domain.Customer) (*domain.Customer, error) {
	if err := customer.Validate(); err != nil {
		return nil, err
	}

	err := c.uow.Do(ctx, func(tx pgx.Tx) error {
		return persistence.NewCustomerRepository(tx).Create(ctx, customer)
	})
	if err != nil {
		return nil, err
	}

	logCustomerEvent(ctx, eventCustomerCreated, "Customer created", customer.OrganizationID, customer.ID)

	return customer, nil
}
