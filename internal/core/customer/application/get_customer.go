package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

type GetCustomerUseCase interface {
	GetCustomer(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Customer, error)
}

type getCustomer struct {
	customers domain.CustomerRepository
}

func NewGetCustomer(customers domain.CustomerRepository) GetCustomerUseCase {
	return &getCustomer{customers: customers}
}

func (g *getCustomer) GetCustomer(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Customer, error) {
	return g.customers.FindByID(ctx, organizationID, id)
}
