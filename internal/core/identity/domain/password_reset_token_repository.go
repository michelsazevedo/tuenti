package domain

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type PasswordResetTokenRepository interface {
	Create(ctx context.Context, token *PasswordResetToken) error
	FindByDigest(ctx context.Context, digest string) (*PasswordResetToken, error)
	MarkUsed(ctx context.Context, id pgtype.UUID) error
	InvalidateActiveForUser(ctx context.Context, userID pgtype.UUID) error
}
