package persistence

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const refreshTokenKeyPrefix = "refresh_token:"

type RefreshTokenStore struct {
	client *goredis.Client
}

func NewRefreshTokenStore(client *goredis.Client) *RefreshTokenStore {
	return &RefreshTokenStore{client: client}
}

func (s *RefreshTokenStore) Save(ctx context.Context, token, userID string, ttl time.Duration) error {
	return s.client.Set(ctx, refreshTokenKeyPrefix+token, userID, ttl).Err()
}
