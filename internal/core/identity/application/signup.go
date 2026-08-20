package application

import (
	"context"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	orgpersistence "github.com/michelsazevedo/tuenti/internal/core/organization/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const emailConfirmationTokenTTL = 24 * time.Hour

type SignupOrganization struct {
	Name              string
	IndustryID        pgtype.UUID
	NumberOfEmployees int
}

type SignupUseCase interface {
	SignUp(ctx context.Context, user *domain.User, org SignupOrganization) error
}

type signup struct {
	uow          *database.UnitOfWork
	publisher    domain.ConfirmationEventPublisher
	industryRepo orgdomain.IndustryRepository
	baseURL      string
}

func NewSignup(
	uow *database.UnitOfWork,
	publisher domain.ConfirmationEventPublisher,
	industryRepo orgdomain.IndustryRepository,
	conf *config.Config,
) SignupUseCase {
	return &signup{
		uow:          uow,
		publisher:    publisher,
		industryRepo: industryRepo,
		baseURL:      conf.Settings.EmailConfirmationBaseURL,
	}
}

func (s *signup) SignUp(ctx context.Context, user *domain.User, org SignupOrganization) error {
	exists, err := s.industryRepo.Exists(ctx, org.IndustryID)
	if err != nil {
		return err
	}

	if !exists {
		return orgdomain.ErrIndustryNotFound
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordDigest = string(hash)
	user.Password = ""

	var rawToken string

	err = s.uow.Do(ctx, func(tx pgx.Tx) error {
		if err := persistence.NewUserRepository(tx).Create(ctx, user); err != nil {
			return err
		}

		orgEntity := &orgdomain.Organization{
			Name:              org.Name,
			IndustryID:        org.IndustryID,
			NumberOfEmployees: org.NumberOfEmployees,
		}
		orgEntity.StartTrial(time.Now().UTC())

		if err := orgpersistence.NewOrganizationRepository(tx).Create(ctx, orgEntity); err != nil {
			return err
		}

		membership := &orgdomain.Membership{OrganizationId: orgEntity.Id, UserId: user.Id, Role: orgdomain.RoleManager}

		if err := orgpersistence.NewMembershipRepository(tx).Create(ctx, membership); err != nil {
			return err
		}

		raw, err := domain.GenerateEmailConfirmationToken()
		if err != nil {
			return err
		}

		rawToken = raw

		return persistence.NewEmailConfirmationTokenRepository(tx).Create(ctx, &domain.EmailConfirmationToken{
			UserID:      user.Id,
			TokenDigest: domain.HashEmailConfirmationToken(raw),
			ExpiresAt:   time.Now().Add(emailConfirmationTokenTTL),
		})
	})
	if err != nil {
		return err
	}

	confirmationURL := s.baseURL + "?token=" + url.QueryEscape(rawToken)

	event := domain.NewEmailConfirmationRequested(user.Id.String(), user.Name, user.Email, confirmationURL)
	if err := s.publisher.PublishEmailConfirmationRequested(ctx, event); err != nil {
		logConfirmationEventPublishFailure(ctx, user, err)
	}

	return nil
}

func logConfirmationEventPublishFailure(ctx context.Context, user *domain.User, err error) {
	logger := observability.Logger(ctx)

	logger.Warn().
		Str("event", "email_confirmation_event_publish_failed").
		Str("user_id", user.Id.String()).
		Str("email", user.Email).
		Err(err).
		Msg("Email confirmation event publish failed; the confirmation email and the issued token are unaffected")
}
