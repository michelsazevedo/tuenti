package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createMembership = `INSERT INTO memberships(organization_id, user_id, role) VALUES($1, $2, $3) RETURNING id`

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
