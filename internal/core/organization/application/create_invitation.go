package application

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
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
	organizations domain.OrganizationRepository
	users         identitydomain.UserRepository
	memberships   domain.MembershipRepository
	invitations   domain.InvitationRepository
	authz         MembershipAuthorizationService
	publisher     domain.InvitationEventPublisher
	baseURL       string
}

func NewCreateInvitation(
	organizations domain.OrganizationRepository,
	users identitydomain.UserRepository,
	memberships domain.MembershipRepository,
	invitations domain.InvitationRepository,
	authz MembershipAuthorizationService,
	publisher domain.InvitationEventPublisher,
	conf *config.Config,
) CreateInvitationUseCase {
	return &createInvitation{
		organizations: organizations,
		users:         users,
		memberships:   memberships,
		invitations:   invitations,
		authz:         authz,
		publisher:     publisher,
		baseURL:       conf.Settings.InvitationBaseURL,
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

	inviterID := authorization.Membership().UserId

	organization, err := c.organizations.FindByID(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	inviter, err := c.users.FindByID(ctx, inviterID)
	if err != nil {
		return nil, err
	}

	if err := c.ensureNotAlreadyMember(ctx, email, organizationID); err != nil {
		return nil, err
	}

	if err := c.ensureNoPendingInvitation(ctx, email, organizationID); err != nil {
		return nil, err
	}

	invitation := &domain.Invitation{
		OrganizationId:        organizationID,
		Email:                 email,
		Role:                  role,
		TokenDigest:           domain.HashInvitationToken(rawToken),
		InvitedByMembershipId: authorization.Membership().Id,
		ExpiresAt:             now.Add(invitationTokenTTL),
	}

	if err := c.invitations.Create(ctx, invitation); err != nil {
		return nil, err
	}

	c.logInvitationCreated(ctx, invitation)
	c.publishInvitationCreated(ctx, invitation, organization.Name, inviterID, inviter.Name, rawToken)

	return invitation, nil
}

func (c *createInvitation) ensureNotAlreadyMember(
	ctx context.Context,
	email string,
	organizationID pgtype.UUID,
) error {
	user, err := c.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, identitydomain.ErrUserNotFound) {
			return nil
		}

		return err
	}

	_, err = c.memberships.FindByUserAndOrganization(ctx, user.Id, organizationID)
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
	email string,
	organizationID pgtype.UUID,
) error {
	_, err := c.invitations.FindPendingByEmailAndOrganization(ctx, email, organizationID)
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

func (c *createInvitation) publishInvitationCreated(
	ctx context.Context,
	invitation *domain.Invitation,
	organizationName string,
	inviterID pgtype.UUID,
	inviterName, rawToken string,
) {
	event := domain.NewOrganizationInvitationCreated(
		invitation.Id.String(),
		invitation.OrganizationId.String(),
		organizationName,
		inviterID.String(),
		inviterName,
		invitation.Email,
		string(invitation.Role),
		c.invitationURL(rawToken),
	)

	if err := c.publisher.PublishOrganizationInvitationCreated(ctx, event); err != nil {
		c.logInvitationEventPublishFailure(ctx, invitation, err)
	}
}

func (c *createInvitation) invitationURL(rawToken string) string {
	return c.baseURL + "?token=" + url.QueryEscape(rawToken)
}

func (c *createInvitation) logInvitationEventPublishFailure(
	ctx context.Context,
	invitation *domain.Invitation,
	err error,
) {
	logger := observability.Logger(ctx)

	logger.Warn().
		Str("event", "invitation_event_publish_failed").
		Str("invitation_id", invitation.Id.String()).
		Str("organization_id", invitation.OrganizationId.String()).
		Err(err).
		Msg("Invitation event publish failed; the issued invitation remains valid for its TTL")
}
