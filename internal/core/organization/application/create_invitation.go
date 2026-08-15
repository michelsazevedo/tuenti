package application

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	identitypersistence "github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const invitationTokenTTL = 7 * 24 * time.Hour

type CreateInvitationUseCase interface {
	CreateInvitation(
		ctx context.Context,
		inviterUserID, organizationID pgtype.UUID,
		email string,
		role domain.Role,
	) (*domain.Invitation, error)
}

type createInvitation struct {
	uow     *database.UnitOfWork
	authz   MembershipAuthorizationService
	mailer  domain.InvitationMailer
	baseURL string
}

func NewCreateInvitation(
	uow *database.UnitOfWork,
	authz MembershipAuthorizationService,
	mailer domain.InvitationMailer,
	conf *config.Config,
) CreateInvitationUseCase {
	return &createInvitation{
		uow:     uow,
		authz:   authz,
		mailer:  mailer,
		baseURL: conf.Settings.InvitationBaseURL,
	}
}

func (c *createInvitation) CreateInvitation(
	ctx context.Context,
	inviterUserID, organizationID pgtype.UUID,
	email string,
	role domain.Role,
) (*domain.Invitation, error) {
	authorization, err := c.authz.Authorize(ctx, inviterUserID, organizationID)
	if err != nil {
		return nil, err
	}

	if !authorization.CanCreateInvitation() || !authorization.CanAssignInvitationRole(role) {
		return nil, domain.ErrInvitationForbidden
	}

	rawToken, err := domain.GenerateInvitationToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	var (
		invitation       *domain.Invitation
		organizationName string
	)

	err = c.uow.Do(ctx, func(tx pgx.Tx) error {
		organization, err := repository.NewOrganizationRepository(tx).FindByID(ctx, organizationID)
		if err != nil {
			return err
		}

		organizationName = organization.Name

		if err := c.ensureNotAlreadyMember(ctx, tx, email, organizationID); err != nil {
			return err
		}

		if err := c.ensureNoPendingInvitation(ctx, tx, email, organizationID); err != nil {
			return err
		}

		invitation = &domain.Invitation{
			OrganizationId:        organizationID,
			Email:                 email,
			Role:                  role,
			TokenDigest:           domain.HashInvitationToken(rawToken),
			InvitedByMembershipId: authorization.Membership().Id,
			ExpiresAt:             now.Add(invitationTokenTTL),
		}

		return repository.NewInvitationRepository(tx).Create(ctx, invitation)
	})
	if err != nil {
		return nil, err
	}

	c.logInvitationCreated(ctx, invitation)
	c.sendInvitation(ctx, invitation, organizationName, rawToken)

	return invitation, nil
}

func (c *createInvitation) ensureNotAlreadyMember(
	ctx context.Context,
	tx pgx.Tx,
	email string,
	organizationID pgtype.UUID,
) error {
	user, err := identitypersistence.NewUserRepository(tx).FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, identitydomain.ErrUserNotFound) {
			return nil
		}

		return err
	}

	_, err = repository.NewMembershipRepository(tx).FindByUserAndOrganization(ctx, user.Id, organizationID)
	if err == nil {
		return domain.ErrAlreadyMember
	}

	if !errors.Is(err, domain.ErrMembershipNotFound) {
		return err
	}

	return nil
}

func (c *createInvitation) ensureNoPendingInvitation(
	ctx context.Context,
	tx pgx.Tx,
	email string,
	organizationID pgtype.UUID,
) error {
	_, err := repository.NewInvitationRepository(tx).FindPendingByEmailAndOrganization(ctx, email, organizationID)
	if err == nil {
		return domain.ErrDuplicateInvitation
	}

	if !errors.Is(err, domain.ErrInvitationNotFound) {
		return err
	}

	return nil
}

func (c *createInvitation) logInvitationCreated(ctx context.Context, invitation *domain.Invitation) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", "invitation_created").
		Str("invitation_id", invitation.Id.String()).
		Str("organization_id", invitation.OrganizationId.String()).
		Str("invited_by_membership_id", invitation.InvitedByMembershipId.String()).
		Str("role", string(invitation.Role)).
		Msg("Invitation created")
}

func (c *createInvitation) sendInvitation(
	ctx context.Context,
	invitation *domain.Invitation,
	organizationName, rawToken string,
) {
	invitationURL := c.baseURL + "?token=" + url.QueryEscape(rawToken)

	err := c.mailer.SendInvitation(ctx, invitation.Email, organizationName, string(invitation.Role), invitationURL)
	if err != nil {
		c.logInvitationEmailFailure(ctx, invitation, err)
	}
}

func (c *createInvitation) logInvitationEmailFailure(ctx context.Context, invitation *domain.Invitation, err error) {
	logger := observability.Logger(ctx)

	logger.Error().
		Str("event", "invitation_email_send_failed").
		Str("invitation_id", invitation.Id.String()).
		Str("organization_id", invitation.OrganizationId.String()).
		Err(err).
		Msg("Invitation email delivery failed; the issued invitation remains valid for its TTL")
}
