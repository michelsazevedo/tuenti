package application

import (
	"context"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

type ListItemsUseCase interface {
	ListItems(ctx context.Context, filter domain.ListItemsFilter) ([]*domain.Item, error)
}

type listItems struct {
	items domain.ItemRepository
}

func NewListItems(items domain.ItemRepository) ListItemsUseCase {
	return &listItems{items: items}
}

func (l *listItems) ListItems(ctx context.Context, filter domain.ListItemsFilter) ([]*domain.Item, error) {
	if filter.Type != nil && !filter.Type.Valid() {
		return nil, domain.ErrInvalidItemType
	}

	return l.items.List(ctx, filter)
}
