package application

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

type UpdateItem struct {
	uow *database.UnitOfWork
}

func NewUpdateItem(uow *database.UnitOfWork) *UpdateItem {
	return &UpdateItem{uow: uow}
}

func (uc *UpdateItem) UpdateItem(ctx context.Context, item *domain.Item) (*domain.Item, error) {
	if err := item.Validate(); err != nil {
		return nil, err
	}

	err := uc.uow.Do(ctx, func(tx pgx.Tx) error {
		repo := persistence.NewItemRepository(tx)

		return repo.Update(ctx, item)
	})
	if err != nil {
		return nil, err
	}

	return item, nil
}
