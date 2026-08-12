package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createOrganization = `INSERT INTO organizations(name) VALUES($1) RETURNING id`

const findOrganizationByID = `SELECT id, name, created_at, updated_at FROM organizations WHERE id = $1`

type OrganizationRepository struct {
	db database.DBTX
}

func NewOrganizationRepository(db database.DBTX) *OrganizationRepository {
	return &OrganizationRepository{db: db}
}

func (r *OrganizationRepository) Create(ctx context.Context, org *domain.Organization) error {
	return r.db.QueryRow(ctx, createOrganization, org.Name).Scan(&org.Id)
}

func (r *OrganizationRepository) FindByID(ctx context.Context, id pgtype.UUID) (*domain.Organization, error) {
	org := &domain.Organization{}

	err := r.db.QueryRow(ctx, findOrganizationByID, id).Scan(
		&org.Id, &org.Name, &org.CreatedAt, &org.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrganizationNotFound
		}

		return nil, err
	}

	return org, nil
}
