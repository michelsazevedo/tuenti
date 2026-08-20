package domain

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type ListItemsFilter struct {
	OrganizationID pgtype.UUID
	Type           *ItemType
	Search         string
}

type ItemRepository interface {
	Create(ctx context.Context, item *Item) error
	Update(ctx context.Context, item *Item) error
	Delete(ctx context.Context, organizationID, id pgtype.UUID) error
	FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*Item, error)
	List(ctx context.Context, filter ListItemsFilter) ([]*Item, error)
}
