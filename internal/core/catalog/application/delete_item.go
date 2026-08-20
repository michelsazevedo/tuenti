package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

type DeleteItem struct {
	repo domain.ItemRepository
}

func NewDeleteItem(repo domain.ItemRepository) *DeleteItem {
	return &DeleteItem{repo: repo}
}

func (uc *DeleteItem) DeleteItem(ctx context.Context, organizationID, id pgtype.UUID) error {
	return uc.repo.Delete(ctx, organizationID, id)
}
