package application

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type RevokeInvitationUseCase interface {
	RevokeInvitation(ctx context.Context, revokerUserID, organizationID, invitationID pgtype.UUID) error
}

type revokeInvitation struct {
	invitations domain.InvitationRepository
	authz       MembershipAuthorizationService
}

func NewRevokeInvitation(
	invitations domain.InvitationRepository,
	authz MembershipAuthorizationService,
) RevokeInvitationUseCase {
	return &revokeInvitation{invitations: invitations, authz: authz}
}

func (u *revokeInvitation) RevokeInvitation(
	ctx context.Context, revokerUserID, organizationID, invitationID pgtype.UUID,
) error {
	authorization, err := u.authz.Authorize(ctx, revokerUserID, organizationID)
	if err != nil {
		return err
	}

	invitation, err := u.invitations.FindByID(ctx, invitationID)
	if err != nil {
		return err
	}

	if invitation.OrganizationId != organizationID {
		return domain.ErrInvitationNotFound
	}

	if !authorization.CanRevokeInvitation(invitation.Role) {
		return domain.ErrInvitationForbidden
	}

	now := time.Now().UTC()

	if invitation.IsAccepted() || invitation.IsRevoked() || invitation.IsExpired(now) {
		return nil
	}

	if err := u.invitations.MarkRevoked(ctx, invitation.Id, now); err != nil {
		return err
	}

	logInvitationRevoked(ctx, invitation, revokerUserID)

	return nil
}

func logInvitationRevoked(ctx context.Context, invitation *domain.Invitation, revokerUserID pgtype.UUID) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", "invitation_revoked").
		Str("invitation_id", invitation.Id.String()).
		Str("organization_id", invitation.OrganizationId.String()).
		Str("role", string(invitation.Role)).
		Str("revoked_by_user_id", revokerUserID.String()).
		Msg("Invitation revoked")
}
