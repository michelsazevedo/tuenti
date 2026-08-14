package domain

import (
	"context"
	"time"
)

type RefreshTokenStore interface {
	Save(ctx context.Context, userID string, ttl time.Duration) (string, error)
	Validate(ctx context.Context, rawToken string) (*RefreshToken, error)
	Rotate(ctx context.Context, rawToken string, ttl time.Duration) (newRawToken string, userID string, err error)
	Revoke(ctx context.Context, rawToken string) error
	RevokeAllForUser(ctx context.Context, userID string) error
}
