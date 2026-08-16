package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const passwordResetTokenTTL = 30 * time.Minute

type RequestPasswordResetUseCase interface {
	RequestPasswordReset(ctx context.Context, email string) error
}

type requestPasswordReset struct {
	users     domain.UserRepository
	tokens    domain.PasswordResetTokenRepository
	publisher domain.PasswordResetEventPublisher
	baseURL   string
}

func NewRequestPasswordReset(
	users domain.UserRepository,
	tokens domain.PasswordResetTokenRepository,
	publisher domain.PasswordResetEventPublisher,
	conf *config.Config,
) RequestPasswordResetUseCase {
	return &requestPasswordReset{
		users:     users,
		tokens:    tokens,
		publisher: publisher,
		baseURL:   conf.Settings.PasswordResetBaseURL,
	}
}

func (s *requestPasswordReset) RequestPasswordReset(ctx context.Context, email string) error {
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil
		}

		return fmt.Errorf("requesting password reset: %w", err)
	}

	if err := s.tokens.InvalidateActiveForUser(ctx, user.Id); err != nil {
		return fmt.Errorf("invalidating active password reset tokens: %w", err)
	}

	rawToken, err := domain.GeneratePasswordResetToken()
	if err != nil {
		return fmt.Errorf("requesting password reset: %w", err)
	}

	token := &domain.PasswordResetToken{
		UserID:      user.Id,
		TokenDigest: domain.HashPasswordResetToken(rawToken),
		ExpiresAt:   time.Now().Add(passwordResetTokenTTL),
	}

	if err := s.tokens.Create(ctx, token); err != nil {
		return fmt.Errorf("creating password reset token: %w", err)
	}

	link := s.baseURL + "?token=" + url.QueryEscape(rawToken)

	event := domain.NewPasswordResetRequested(user.Id.String(), user.Name, user.Email, link)
	if err := s.publisher.PublishPasswordResetRequested(ctx, event); err != nil {
		logPasswordResetEventPublishFailure(ctx, user, err)
	}

	return nil
}

func logPasswordResetEventPublishFailure(ctx context.Context, user *domain.User, err error) {
	logger := observability.Logger(ctx)

	logger.Warn().
		Str("event", "password_reset_event_publish_failed").
		Str("user_id", user.Id.String()).
		Str("email", user.Email).
		Err(err).
		Msg("Password reset event publish failed; the reset email and the issued token are unaffected")
}
