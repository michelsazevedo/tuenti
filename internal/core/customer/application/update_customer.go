package application

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/core/customer/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

type UpdateCustomer struct {
	uow *database.UnitOfWork
}

func NewUpdateCustomer(uow *database.UnitOfWork) *UpdateCustomer {
	return &UpdateCustomer{uow: uow}
}

func (uc *UpdateCustomer) UpdateCustomer(ctx context.Context, customer *domain.Customer) (*domain.Customer, error) {
	if err := customer.Validate(); err != nil {
		return nil, err
	}

	err := uc.uow.Do(ctx, func(tx pgx.Tx) error {
		return persistence.NewCustomerRepository(tx).Update(ctx, customer)
	})
	if err != nil {
		return nil, err
	}

	logCustomerEvent(ctx, eventCustomerUpdated, "Customer updated", customer.OrganizationID, customer.ID)

	return customer, nil
}
