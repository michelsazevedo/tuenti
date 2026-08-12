package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type GetOrganizationByIDUseCase interface {
	GetByID(ctx context.Context, id pgtype.UUID) (*domain.Organization, error)
}

type getOrganizationByID struct {
	organizations domain.OrganizationRepository
}

func NewGetOrganizationByID(organizations domain.OrganizationRepository) GetOrganizationByIDUseCase {
	return &getOrganizationByID{organizations: organizations}
}

func (u *getOrganizationByID) GetByID(ctx context.Context, id pgtype.UUID) (*domain.Organization, error) {
	return u.organizations.FindByID(ctx, id)
}
