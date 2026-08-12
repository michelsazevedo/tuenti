package domain

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type OrganizationRepository interface {
	Create(ctx context.Context, org *Organization) error
	FindByID(ctx context.Context, id pgtype.UUID) (*Organization, error)
}

type MembershipRepository interface {
	Create(ctx context.Context, membership *Membership) error
}
