package application

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type CreateItemUseCase interface {
	CreateItem(ctx context.Context, item *domain.Item) (*domain.Item, error)
}

type createItem struct {
	uow *database.UnitOfWork
}

func NewCreateItem(uow *database.UnitOfWork) CreateItemUseCase {
	return &createItem{uow: uow}
}

func (c *createItem) CreateItem(ctx context.Context, item *domain.Item) (*domain.Item, error) {
	if err := item.Validate(); err != nil {
		return nil, err
	}

	err := c.uow.Do(ctx, func(tx pgx.Tx) error {
		return persistence.NewItemRepository(tx).Create(ctx, item)
	})
	if err != nil {
		return nil, err
	}

	logItemCreated(ctx, item)

	return item, nil
}

func logItemCreated(ctx context.Context, item *domain.Item) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", "item_created").
		Str("item_id", item.ID.String()).
		Str("organization_id", item.OrganizationID.String()).
		Str("type", string(item.Type)).
		Int("taxes", len(item.Taxes)).
		Msg("Item created")
}
