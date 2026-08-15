package application

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type RevokeInvitationUseCase interface {
	RevokeInvitation(ctx context.Context, revokerUserID, organizationID, invitationID pgtype.UUID) error
}

type revokeInvitation struct {
	uow   *database.UnitOfWork
	authz MembershipAuthorizationService
}

func NewRevokeInvitation(uow *database.UnitOfWork, authz MembershipAuthorizationService) RevokeInvitationUseCase {
	return &revokeInvitation{uow: uow, authz: authz}
}

func (u *revokeInvitation) RevokeInvitation(
	ctx context.Context, revokerUserID, organizationID, invitationID pgtype.UUID,
) error {
	authorization, err := u.authz.Authorize(ctx, revokerUserID, organizationID)
	if err != nil {
		return err
	}

	return u.uow.Do(ctx, func(tx pgx.Tx) error {
		invitations := repository.NewInvitationRepository(tx)

		invitation, err := invitations.FindByID(ctx, invitationID)
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

		if err := invitations.MarkRevoked(ctx, invitation.Id, now); err != nil {
			return err
		}

		logInvitationRevoked(ctx, invitation, revokerUserID)

		return nil
	})
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
