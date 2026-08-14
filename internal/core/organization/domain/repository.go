package domain

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type OrganizationRepository interface {
	Create(ctx context.Context, org *Organization) error
	FindByID(ctx context.Context, id pgtype.UUID) (*Organization, error)
	FindExpiredTrials(ctx context.Context, now time.Time, limit int) ([]*Organization, error)
	UpdateSubscriptionStatus(ctx context.Context, id pgtype.UUID, status SubscriptionStatus) error
}

type MembershipRepository interface {
	Create(ctx context.Context, membership *Membership) error
	FindByUserID(ctx context.Context, userID pgtype.UUID) (*Membership, error)
	FindByUserAndOrganization(ctx context.Context, userID, organizationID pgtype.UUID) (*Membership, error)
}
