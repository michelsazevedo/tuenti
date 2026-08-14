package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type ConfirmPasswordResetUseCase interface {
	ConfirmPasswordReset(ctx context.Context, rawToken, newPassword string) error
}

type confirmPasswordReset struct {
	uow          *database.UnitOfWork
	tokens       domain.PasswordResetTokenRepository
	refreshStore domain.RefreshTokenStore
}

func NewConfirmPasswordReset(
	uow *database.UnitOfWork,
	tokens domain.PasswordResetTokenRepository,
	refreshStore domain.RefreshTokenStore,
) ConfirmPasswordResetUseCase {
	return &confirmPasswordReset{uow: uow, tokens: tokens, refreshStore: refreshStore}
}

func (s *confirmPasswordReset) ConfirmPasswordReset(ctx context.Context, rawToken, newPassword string) error {
	digest := domain.HashPasswordResetToken(rawToken)

	token, err := s.tokens.FindByDigest(ctx, digest)
	if err != nil {
		return err
	}

	if err := token.Validate(time.Now()); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing the new password: %w", err)
	}

	err = s.uow.Do(ctx, func(tx pgx.Tx) error {
		if err := persistence.NewUserRepository(tx).UpdatePasswordDigest(ctx, token.UserID, string(hash)); err != nil {
			return err
		}

		return persistence.NewPasswordResetTokenRepository(tx).MarkUsed(ctx, token.Id)
	})
	if err != nil {
		return fmt.Errorf("confirming password reset: %w", err)
	}

	if err := s.refreshStore.RevokeAllForUser(ctx, token.UserID.String()); err != nil {
		logPasswordResetSessionRevocationFailure(ctx, token.UserID.String(), err)
	}

	return nil
}

func logPasswordResetSessionRevocationFailure(ctx context.Context, userID string, err error) {
	logger := observability.Logger(ctx)

	logger.Error().
		Str("event", "password_reset_session_revocation_failed").
		Str("user_id", userID).
		Err(err).
		Msg("Password was reset but the user's refresh sessions survived; they must be revoked manually")
}
