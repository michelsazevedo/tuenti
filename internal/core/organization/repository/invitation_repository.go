package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const invitationColumns = `id, organization_id, email, role, token_digest, invited_by_membership_id,
	created_at, expires_at, accepted_at, revoked_at`

const createInvitation = `INSERT INTO invitations(organization_id, email, role, token_digest, invited_by_membership_id, expires_at)
VALUES($1, $2, $3, $4, $5, $6) RETURNING id, created_at`

const findInvitationByID = `SELECT ` + invitationColumns + `
FROM invitations WHERE id = $1`

const findInvitationByTokenDigest = `SELECT ` + invitationColumns + `
FROM invitations WHERE token_digest = $1`

const findPendingInvitationByEmailAndOrganization = `SELECT ` + invitationColumns + `
FROM invitations
WHERE lower(email) = lower($1) AND organization_id = $2
	AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()`

const findInvitationsByOrganization = `SELECT ` + invitationColumns + `
FROM invitations WHERE organization_id = $1 ORDER BY created_at DESC`

const markInvitationAccepted = `UPDATE invitations SET accepted_at = $2 WHERE id = $1`

const markInvitationRevoked = `UPDATE invitations SET revoked_at = $2 WHERE id = $1`

const pendingInvitationIndex = "invitations_pending_organization_id_email_idx"

type InvitationRepository struct {
	db database.DBTX
}

func NewInvitationRepository(db database.DBTX) *InvitationRepository {
	return &InvitationRepository{db: db}
}

func (r *InvitationRepository) Create(ctx context.Context, invitation *domain.Invitation) error {
	err := r.db.QueryRow(ctx, createInvitation,
		invitation.OrganizationId, invitation.Email, invitation.Role,
		invitation.TokenDigest, invitation.InvitedByMembershipId, invitation.ExpiresAt,
	).Scan(&invitation.Id, &invitation.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode &&
			pgErr.ConstraintName == pendingInvitationIndex {
			return domain.ErrDuplicateInvitation
		}

		return err
	}

	return nil
}

func (r *InvitationRepository) FindByID(ctx context.Context, id pgtype.UUID) (*domain.Invitation, error) {
	return r.findOne(ctx, findInvitationByID, id)
}

func (r *InvitationRepository) FindByTokenDigest(ctx context.Context, tokenDigest string) (*domain.Invitation, error) {
	return r.findOne(ctx, findInvitationByTokenDigest, tokenDigest)
}

func (r *InvitationRepository) FindPendingByEmailAndOrganization(
	ctx context.Context,
	email string,
	organizationID pgtype.UUID,
) (*domain.Invitation, error) {
	return r.findOne(ctx, findPendingInvitationByEmailAndOrganization, email, organizationID)
}

func (r *InvitationRepository) FindByOrganization(ctx context.Context, organizationID pgtype.UUID) ([]*domain.Invitation, error) {
	rows, err := r.db.Query(ctx, findInvitationsByOrganization, organizationID)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Invitation, error) {
		invitation := &domain.Invitation{}

		if err := scanInvitation(row, invitation); err != nil {
			return nil, err
		}

		return invitation, nil
	})
}

func (r *InvitationRepository) MarkAccepted(ctx context.Context, id pgtype.UUID, acceptedAt time.Time) error {
	return r.markTimestamp(ctx, markInvitationAccepted, id, acceptedAt)
}

func (r *InvitationRepository) MarkRevoked(ctx context.Context, id pgtype.UUID, revokedAt time.Time) error {
	return r.markTimestamp(ctx, markInvitationRevoked, id, revokedAt)
}

func (r *InvitationRepository) findOne(ctx context.Context, sql string, args ...any) (*domain.Invitation, error) {
	invitation := &domain.Invitation{}

	if err := scanInvitation(r.db.QueryRow(ctx, sql, args...), invitation); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvitationNotFound
		}

		return nil, err
	}

	return invitation, nil
}

func (r *InvitationRepository) markTimestamp(ctx context.Context, sql string, id pgtype.UUID, at time.Time) error {
	tag, err := r.db.Exec(ctx, sql, id, at)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrInvitationNotFound
	}

	return nil
}

func scanInvitation(row pgx.Row, invitation *domain.Invitation) error {
	return row.Scan(
		&invitation.Id, &invitation.OrganizationId, &invitation.Email, &invitation.Role,
		&invitation.TokenDigest, &invitation.InvitedByMembershipId, &invitation.CreatedAt,
		&invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.RevokedAt,
	)
}
