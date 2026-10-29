package application

import (
	"context"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

type ListCustomersUseCase interface {
	ListCustomers(ctx context.Context, filter domain.ListCustomersFilter) ([]*domain.Customer, error)
}

type listCustomers struct {
	customers domain.CustomerRepository
}

func NewListCustomers(customers domain.CustomerRepository) ListCustomersUseCase {
	return &listCustomers{customers: customers}
}

func (l *listCustomers) ListCustomers(ctx context.Context, filter domain.ListCustomersFilter) ([]*domain.Customer, error) {
	return l.customers.List(ctx, filter)
}
