package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

type ConfirmEmailUseCase interface {
	ConfirmEmail(ctx context.Context, rawToken string) error
}

type confirmEmail struct {
	uow    *database.UnitOfWork
	tokens domain.EmailConfirmationTokenRepository
	users  domain.UserRepository
}

func NewConfirmEmail(
	uow *database.UnitOfWork,
	tokens domain.EmailConfirmationTokenRepository,
	users domain.UserRepository,
) ConfirmEmailUseCase {
	return &confirmEmail{uow: uow, tokens: tokens, users: users}
}

func (s *confirmEmail) ConfirmEmail(ctx context.Context, rawToken string) error {
	digest := domain.HashEmailConfirmationToken(rawToken)

	token, err := s.tokens.FindByDigest(ctx, digest)
	if err != nil {
		return err
	}

	if token.IsExpired(time.Now()) {
		return domain.ErrEmailConfirmationTokenExpired
	}

	if token.IsUsed() {
		return s.replayOutcome(ctx, token.UserID)
	}

	err = s.uow.Do(ctx, func(tx pgx.Tx) error {
		if err := persistence.NewUserRepository(tx).MarkConfirmed(ctx, token.UserID, time.Now().UTC()); err != nil {
			return err
		}

		return persistence.NewEmailConfirmationTokenRepository(tx).MarkUsed(ctx, token.Id)
	})
	if err != nil {
		return fmt.Errorf("confirming email: %w", err)
	}

	return nil
}

func (s *confirmEmail) replayOutcome(ctx context.Context, userID pgtype.UUID) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}

	if !user.IsConfirmed() {
		return domain.ErrEmailConfirmationTokenUsed
	}

	return nil
}
