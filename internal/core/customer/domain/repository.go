package domain

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type ListCustomersFilter struct {
	OrganizationID pgtype.UUID
	Search         string
}

type CustomerRepository interface {
	Create(ctx context.Context, customer *Customer) error
	Update(ctx context.Context, customer *Customer) error
	Delete(ctx context.Context, organizationID, id pgtype.UUID) error
	FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*Customer, error)
	List(ctx context.Context, filter ListCustomersFilter) ([]*Customer, error)
}
