package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

type GetItemUseCase interface {
	GetItem(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Item, error)
}

type getItem struct {
	items domain.ItemRepository
}

func NewGetItem(items domain.ItemRepository) GetItemUseCase {
	return &getItem{items: items}
}

func (g *getItem) GetItem(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Item, error) {
	return g.items.FindByID(ctx, organizationID, id)
}
