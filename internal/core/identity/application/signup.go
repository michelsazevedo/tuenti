package application

import (
	"context"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	orgrepository "github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const emailConfirmationTokenTTL = 24 * time.Hour

type SignupUseCase interface {
	SignUp(ctx context.Context, user *domain.User, organizationName string) error
}

type signup struct {
	uow       *database.UnitOfWork
	publisher domain.ConfirmationEventPublisher
	baseURL   string
}

func NewSignup(
	uow *database.UnitOfWork,
	publisher domain.ConfirmationEventPublisher,
	conf *config.Config,
) SignupUseCase {
	return &signup{
		uow:       uow,
		publisher: publisher,
		baseURL:   conf.Settings.EmailConfirmationBaseURL,
	}
}

func (s *signup) SignUp(ctx context.Context, user *domain.User, organizationName string) error {
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

		org := &orgdomain.Organization{Name: organizationName}
		org.StartTrial(time.Now().UTC())

		if err := orgrepository.NewOrganizationRepository(tx).Create(ctx, org); err != nil {
			return err
		}

		membership := &orgdomain.Membership{OrganizationId: org.Id, UserId: user.Id, Role: orgdomain.RoleManager}

		if err := orgrepository.NewMembershipRepository(tx).Create(ctx, membership); err != nil {
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
