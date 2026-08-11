package domain

import (
	"context"
	"time"
)

type RefreshTokenStore interface {
	Save(ctx context.Context, token, userID string, ttl time.Duration) error
}
