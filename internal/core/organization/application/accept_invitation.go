package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	identitypersistence "github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type AcceptInvitationUseCase interface {
	AcceptInvitation(ctx context.Context, token string, name, password *string) (*domain.Membership, error)
}

type acceptInvitation struct {
	uow *database.UnitOfWork
}

func NewAcceptInvitation(uow *database.UnitOfWork) AcceptInvitationUseCase {
	return &acceptInvitation{uow: uow}
}

func (a *acceptInvitation) AcceptInvitation(
	ctx context.Context,
	token string,
	name, password *string,
) (*domain.Membership, error) {
	tokenDigest := domain.HashInvitationToken(token)
	now := time.Now().UTC()

	var membership *domain.Membership

	err := a.uow.Do(ctx, func(tx pgx.Tx) error {
		repositories := newAcceptInvitationRepositories(tx)

		invitation, err := repositories.invitations.FindByTokenDigest(ctx, tokenDigest)
		if err != nil {
			return err
		}

		user, err := repositories.users.FindByEmail(ctx, invitation.Email)

		switch {
		case err == nil:
			membership, err = repositories.acceptAsExistingUser(ctx, invitation, user, name, password, now)

			return err
		case errors.Is(err, identitydomain.ErrUserNotFound):
			membership, err = repositories.acceptAsNewUser(ctx, invitation, name, password, now)

			return err
		default:
			return err
		}
	})
	if err != nil {
		return nil, err
	}

	return membership, nil
}

type acceptInvitationRepositories struct {
	invitations *persistence.InvitationRepository
	memberships *persistence.MembershipRepository
	users       *identitypersistence.UserRepository
}

func newAcceptInvitationRepositories(tx pgx.Tx) *acceptInvitationRepositories {
	return &acceptInvitationRepositories{
		invitations: persistence.NewInvitationRepository(tx),
		memberships: persistence.NewMembershipRepository(tx),
		users:       identitypersistence.NewUserRepository(tx),
	}
}

func (r *acceptInvitationRepositories) acceptAsExistingUser(
	ctx context.Context,
	invitation *domain.Invitation,
	user *identitydomain.User,
	name, password *string,
	now time.Time,
) (*domain.Membership, error) {
	if name != nil || password != nil {
		return nil, domain.ErrInvitationEmailAlreadyRegistered
	}

	existing, err := r.memberships.FindByUserAndOrganization(ctx, user.Id, invitation.OrganizationId)

	switch {
	case err == nil:
		if invitation.AcceptedAt == nil {
			if err := r.invitations.MarkAccepted(ctx, invitation.Id, now); err != nil {
				return nil, err
			}
		}

		return existing, nil
	case !errors.Is(err, domain.ErrMembershipNotFound):
		return nil, err
	}

	if err := invitation.Validate(now); err != nil {
		return nil, err
	}

	return r.joinOrganization(ctx, invitation, user.Id, now, false)
}

func (r *acceptInvitationRepositories) acceptAsNewUser(
	ctx context.Context,
	invitation *domain.Invitation,
	name, password *string,
	now time.Time,
) (*domain.Membership, error) {
	if name == nil || password == nil || strings.TrimSpace(*name) == "" || strings.TrimSpace(*password) == "" {
		return nil, domain.ErrInvitationSignupFieldsRequired
	}

	if err := invitation.Validate(now); err != nil {
		return nil, err
	}

	user, err := r.createConfirmedUser(ctx, *name, invitation.Email, *password, now)
	if err != nil {
		return nil, err
	}

	return r.joinOrganization(ctx, invitation, user.Id, now, true)
}

func (r *acceptInvitationRepositories) createConfirmedUser(
	ctx context.Context,
	name, email, password string,
	now time.Time,
) (*identitydomain.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	confirmedAt := now
	user := &identitydomain.User{
		Name:           name,
		Email:          email,
		PasswordDigest: string(hash),
		ConfirmedAt:    &confirmedAt,
	}

	if err := r.users.Create(ctx, user); err != nil {
		return nil, err
	}

	if err := r.users.MarkConfirmed(ctx, user.Id, confirmedAt); err != nil {
		return nil, err
	}

	return user, nil
}

func (r *acceptInvitationRepositories) joinOrganization(
	ctx context.Context,
	invitation *domain.Invitation,
	userID pgtype.UUID,
	now time.Time,
	newUser bool,
) (*domain.Membership, error) {
	membership := &domain.Membership{
		OrganizationId: invitation.OrganizationId,
		UserId:         userID,
		Role:           invitation.Role,
	}

	if err := r.memberships.Create(ctx, membership); err != nil {
		return nil, err
	}

	if err := r.invitations.MarkAccepted(ctx, invitation.Id, now); err != nil {
		return nil, err
	}

	logInvitationAccepted(ctx, invitation, membership, newUser)

	return membership, nil
}

func logInvitationAccepted(ctx context.Context, invitation *domain.Invitation, membership *domain.Membership, newUser bool) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", "invitation_accepted").
		Str("invitation_id", invitation.Id.String()).
		Str("organization_id", invitation.OrganizationId.String()).
		Str("membership_id", membership.Id.String()).
		Str("role", string(invitation.Role)).
		Bool("new_user", newUser).
		Msg("Invitation accepted")
}
