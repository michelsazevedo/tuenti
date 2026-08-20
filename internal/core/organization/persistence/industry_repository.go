package persistence

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const industryExists = `SELECT EXISTS(SELECT 1 FROM industries WHERE id = $1)`

type IndustryRepository struct {
	db database.DBTX
}

func NewIndustryRepository(db database.DBTX) *IndustryRepository {
	return &IndustryRepository{db: db}
}

func (r *IndustryRepository) Exists(ctx context.Context, id pgtype.UUID) (bool, error) {
	var exists bool

	err := r.db.QueryRow(ctx, industryExists, id).Scan(&exists)

	return exists, err
}
