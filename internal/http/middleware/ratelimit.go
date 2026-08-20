package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	defaultKeyPrefix = "ratelimit"

	counterTimeout = 250 * time.Millisecond

	maxPeekedBodySize = 64 << 10

	maxEmailKeyLength = 254
)

type KeyExtractor func(c echo.Context) (string, error)

type RateLimitConfig struct {
	Max       int
	Window    time.Duration
	KeyPrefix string
	Extractor KeyExtractor
}

func RateLimit(client *goredis.Client, cfg RateLimitConfig) echo.MiddlewareFunc {
	cfg = cfg.normalized()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key, err := cfg.Extractor(c)
			if err != nil {
				log.Warn().Err(err).
					Str("event", "rate_limit_key_unavailable").
					Str("limiter", cfg.KeyPrefix).
					Msg("no rate-limit key, serving request uncounted")

				return next(c)
			}

			count, err := increment(c.Request().Context(), client, cfg.KeyPrefix+":"+key, cfg.Window)
			if err != nil {
				log.Error().Err(err).
					Str("event", "rate_limit_check_failed").
					Str("limiter", cfg.KeyPrefix).
					Msg("redis unavailable, failing open")

				return next(c)
			}

			if count > int64(cfg.Max) {
				log.Warn().
					Str("event", "rate_limit_exceeded").
					Str("limiter", cfg.KeyPrefix).
					Str("remote_ip", c.RealIP()).
					Msg("request rejected over rate limit")

				return echo.NewHTTPError(http.StatusTooManyRequests, "too many requests")
			}

			return next(c)
		}
	}
}

func (cfg RateLimitConfig) normalized() RateLimitConfig {
	if cfg.Max <= 0 {
		panic("middleware: RateLimit needs a positive Max")
	}

	if cfg.Window < time.Millisecond {
		panic("middleware: RateLimit needs a Window of at least a millisecond")
	}

	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = defaultKeyPrefix
	}

	if cfg.Extractor == nil {
		cfg.Extractor = IPKeyExtractor
	}

	return cfg
}

func increment(ctx context.Context, client *goredis.Client, key string, window time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, counterTimeout)
	defer cancel()

	var count *goredis.IntCmd

	if _, err := client.Pipelined(ctx, func(pipe goredis.Pipeliner) error {
		count = pipe.Incr(ctx, key)
		pipe.Do(ctx, "pexpire", key, window.Milliseconds(), "nx")

		return nil
	}); err != nil {
		return 0, err
	}

	return count.Val(), nil
}

func IPKeyExtractor(c echo.Context) (string, error) {
	return c.RealIP(), nil
}

func EmailAndIPKeyExtractor(c echo.Context) (string, error) {
	email, found := peekEmail(c.Request())
	if !found {
		return c.RealIP(), nil
	}

	return c.RealIP() + ":" + email, nil
}

func OrganizationKeyExtractor(c echo.Context) (string, error) {
	identity, err := IdentityFromContext(c.Request().Context())
	if err != nil {
		return "", err
	}

	return identity.CurrentOrganizationID.String(), nil
}

func peekEmail(request *http.Request) (string, bool) {
	if request.Body == nil {
		return "", false
	}

	original := request.Body

	head, err := io.ReadAll(io.LimitReader(original, maxPeekedBodySize+1))

	request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(head), original))

	if err != nil || len(head) > maxPeekedBodySize {
		return "", false
	}

	var payload struct {
		Email string `json:"email"`
	}

	if err := json.Unmarshal(head, &payload); err != nil {
		return "", false
	}

	return normalizeEmail(payload.Email)
}

func normalizeEmail(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))

	if email == "" || len(email) > maxEmailKeyLength {
		return "", false
	}

	return email, true
}
