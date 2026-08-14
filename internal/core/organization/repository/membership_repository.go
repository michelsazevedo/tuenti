package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createMembership = `INSERT INTO memberships(organization_id, user_id, role) VALUES($1, $2, $3) RETURNING id`

const findMembershipByUserID = `SELECT id, organization_id, user_id, role, created_at, updated_at
	FROM memberships WHERE user_id = $1 ORDER BY created_at, id LIMIT 1`

const findMembershipByUserAndOrganization = `SELECT id, organization_id, user_id, role, created_at, updated_at
	FROM memberships WHERE user_id = $1 AND organization_id = $2`

const uniqueViolationCode = "23505"

type MembershipRepository struct {
	db database.DBTX
}

func NewMembershipRepository(db database.DBTX) *MembershipRepository {
	return &MembershipRepository{db: db}
}

func (r *MembershipRepository) Create(ctx context.Context, membership *domain.Membership) error {
	err := r.db.QueryRow(ctx, createMembership,
		membership.OrganizationId, membership.UserId, membership.Role,
	).Scan(&membership.Id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return domain.ErrMembershipAlreadyExists
		}

		return err
	}

	return nil
}

func (r *MembershipRepository) FindByUserID(ctx context.Context, userID pgtype.UUID) (*domain.Membership, error) {
	membership := &domain.Membership{}

	err := r.db.QueryRow(ctx, findMembershipByUserID, userID).Scan(
		&membership.Id, &membership.OrganizationId, &membership.UserId,
		&membership.Role, &membership.CreatedAt, &membership.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrMembershipNotFound
		}

		return nil, err
	}

	return membership, nil
}

func (r *MembershipRepository) FindByUserAndOrganization(
	ctx context.Context,
	userID, organizationID pgtype.UUID,
) (*domain.Membership, error) {
	membership := &domain.Membership{}

	err := r.db.QueryRow(ctx, findMembershipByUserAndOrganization, userID, organizationID).Scan(
		&membership.Id, &membership.OrganizationId, &membership.UserId,
		&membership.Role, &membership.CreatedAt, &membership.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrMembershipNotFound
		}

		return nil, err
	}

	return membership, nil
}
