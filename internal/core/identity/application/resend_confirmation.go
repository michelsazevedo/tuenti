package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

type ResendConfirmationUseCase interface {
	ResendConfirmation(ctx context.Context, email string) error
}

type resendConfirmation struct {
	users   domain.UserRepository
	tokens  domain.EmailConfirmationTokenRepository
	mailer  domain.ConfirmationMailer
	baseURL string
}

func NewResendConfirmation(
	users domain.UserRepository,
	tokens domain.EmailConfirmationTokenRepository,
	mailer domain.ConfirmationMailer,
	conf *config.Config,
) ResendConfirmationUseCase {
	return &resendConfirmation{users: users, tokens: tokens, mailer: mailer, baseURL: conf.Settings.EmailConfirmationBaseURL}
}

func (s *resendConfirmation) ResendConfirmation(ctx context.Context, email string) error {
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil
		}

		return fmt.Errorf("resending confirmation: %w", err)
	}

	if user.IsConfirmed() {
		return nil
	}

	if err := s.tokens.InvalidateActiveForUser(ctx, user.Id); err != nil {
		return fmt.Errorf("invalidating active email confirmation tokens: %w", err)
	}

	rawToken, err := domain.GenerateEmailConfirmationToken()
	if err != nil {
		return fmt.Errorf("resending confirmation: %w", err)
	}

	token := &domain.EmailConfirmationToken{
		UserID:      user.Id,
		TokenDigest: domain.HashEmailConfirmationToken(rawToken),
		ExpiresAt:   time.Now().Add(emailConfirmationTokenTTL),
	}

	if err := s.tokens.Create(ctx, token); err != nil {
		return fmt.Errorf("creating email confirmation token: %w", err)
	}

	confirmationURL := s.baseURL + "?token=" + url.QueryEscape(rawToken)

	if err := s.mailer.SendWelcomeConfirmation(ctx, user.Email, confirmationURL); err != nil {
		logConfirmationEmailFailure(ctx, user, err)
	}

	return nil
}
