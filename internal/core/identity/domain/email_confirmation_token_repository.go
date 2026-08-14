package domain

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type EmailConfirmationTokenRepository interface {
	Create(ctx context.Context, token *EmailConfirmationToken) error
	FindByDigest(ctx context.Context, digest string) (*EmailConfirmationToken, error)
	MarkUsed(ctx context.Context, id pgtype.UUID) error
	InvalidateActiveForUser(ctx context.Context, userID pgtype.UUID) error
}
