package persistence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const (
	refreshTokenKeyPrefix        = "refresh_token:"
	refreshFamilyKeyPrefix       = "refresh_family:"
	refreshUsedKeyPrefix         = "refresh_token_used:"
	refreshUserSessionsKeyPrefix = "user_sessions:"
)

const (
	fieldUserID          = "user_id"
	fieldFamilyID        = "family_id"
	fieldExpiresAt       = "expires_at"
	fieldFamilyExpiresAt = "family_expires_at"
)

const familyIDEntropyBytes = 16

const rotationGraceWindow = 10 * time.Second

const familyAbsoluteLifetime = 7 * 24 * time.Hour

const (
	rotateOK      = "ok"
	rotateInvalid = "invalid"
	rotateReused  = "reused"
	rotateExpired = "expired"
	rotateRace    = "race"
)

const (
	revokeMiss       = "miss"
	revokeLive       = "live"
	revokeSuperseded = "superseded"
)

const luaParseTombstone = `
local function parse_tombstone(value)
  local family_id, rotated_at = string.match(value, '^(.-):(%d+)$')
  if not family_id then
    return value, nil
  end

  return family_id, tonumber(rotated_at)
end
`

const luaRedisNow = `
local function redis_now()
  return tonumber(redis.call('TIME')[1])
end
`

var rotateScript = goredis.NewScript(luaParseTombstone + luaRedisNow + `
local token_key = KEYS[1]
local used_key  = KEYS[2]
local new_key   = KEYS[3]

local new_hash     = ARGV[1]
local ttl          = tonumber(ARGV[2])
local expires_at   = tonumber(ARGV[3])
local now          = tonumber(ARGV[4])
local family_pfx   = ARGV[5]
local token_pfx    = ARGV[6]
local grace        = tonumber(ARGV[7])
local default_family_expiry = tonumber(ARGV[8])
local session_pfx  = ARGV[9]

local record = redis.call('HMGET', token_key, 'user_id', 'family_id', 'expires_at', 'family_expires_at')
local user_id       = record[1]
local family_id     = record[2]
local old_expiry    = record[3]
local family_expiry = tonumber(record[4]) or default_family_expiry

if not family_id then
  local tombstone = redis.call('GET', used_key)
  if not tombstone then
    return {'invalid'}
  end

  local tombstoned_family, rotated_at = parse_tombstone(tombstone)

  if rotated_at then
    local age = redis_now() - rotated_at

    if age >= 0 and age <= grace then
      return {'race', tombstoned_family}
    end
  end

  local family_key = family_pfx .. tombstoned_family
  local live_hash = redis.call('GET', family_key)
  if live_hash then
    local victim_user = redis.call('HGET', token_pfx .. live_hash, 'user_id')
    if victim_user then
      redis.call('SREM', session_pfx .. victim_user, tombstoned_family)
    end
    redis.call('DEL', token_pfx .. live_hash)
  end
  redis.call('DEL', family_key)

  return {'reused', tombstoned_family}
end

if now >= tonumber(old_expiry) or now >= family_expiry then
  redis.call('DEL', token_key)
  return {'expired'}
end

local family_key = family_pfx .. family_id

local remaining = family_expiry - now
if ttl > remaining then
  ttl = remaining
end
if expires_at > family_expiry then
  expires_at = family_expiry
end

redis.call('DEL', token_key)
redis.call('SET', used_key, family_id .. ':' .. redis_now(), 'EX', tonumber(old_expiry) - now)

redis.call('HSET', new_key,
  'user_id', user_id,
  'family_id', family_id,
  'expires_at', expires_at,
  'family_expires_at', family_expiry)
redis.call('EXPIRE', new_key, ttl)
redis.call('SET', family_key, new_hash, 'EX', ttl)

return {'ok', user_id, family_id}
`)

var revokeScript = goredis.NewScript(luaParseTombstone + `
local token_key = KEYS[1]
local used_key  = KEYS[2]

local family_pfx  = ARGV[1]
local token_pfx   = ARGV[2]
local session_pfx = ARGV[3]

local outcome   = 'live'
local family_id = redis.call('HGET', token_key, 'family_id')
local user_id   = nil

if family_id then
  user_id = redis.call('HGET', token_key, 'user_id')
else
  local tombstone = redis.call('GET', used_key)
  if not tombstone then
    return {'miss'}
  end

  family_id = parse_tombstone(tombstone)
  outcome = 'superseded'
end

local family_key = family_pfx .. family_id
local live_hash = redis.call('GET', family_key)
if live_hash then
  if not user_id then
    user_id = redis.call('HGET', token_pfx .. live_hash, 'user_id')
  end
  redis.call('DEL', token_pfx .. live_hash)
end

redis.call('DEL', family_key, token_key)

if user_id then
  redis.call('SREM', session_pfx .. user_id, family_id)
end

return {outcome, family_id}
`)

var revokeAllScript = goredis.NewScript(`
local session_key = KEYS[1]
local family_pfx  = ARGV[1]
local token_pfx   = ARGV[2]

local family_ids = redis.call('SMEMBERS', session_key)
for _, family_id in ipairs(family_ids) do
  local family_key = family_pfx .. family_id
  local live_hash = redis.call('GET', family_key)
  if live_hash then
    redis.call('DEL', token_pfx .. live_hash)
  end
  redis.call('DEL', family_key)
end
redis.call('DEL', session_key)
return #family_ids
`)

type RefreshTokenStore struct {
	client *goredis.Client
}

func NewRefreshTokenStore(client *goredis.Client) *RefreshTokenStore {
	return &RefreshTokenStore{client: client}
}

func (s *RefreshTokenStore) Save(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	if err := checkTTL(ttl); err != nil {
		return "", fmt.Errorf("saving refresh token: %w", err)
	}

	rawToken, err := domain.GenerateRefreshToken()
	if err != nil {
		return "", fmt.Errorf("saving refresh token: %w", err)
	}

	familyID, err := newFamilyID()
	if err != nil {
		return "", fmt.Errorf("saving refresh token: %w", err)
	}

	tokenHash := domain.HashRefreshToken(rawToken)

	now := time.Now()
	familyExpiresAt := now.Add(familyAbsoluteLifetime)
	expiresAt := now.Add(ttl)
	keyTTL := ttl

	if expiresAt.After(familyExpiresAt) {
		expiresAt, keyTTL = familyExpiresAt, familyAbsoluteLifetime
	}

	_, err = s.client.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		pipe.HSet(ctx, tokenKey(tokenHash),
			fieldUserID, userID,
			fieldFamilyID, familyID,
			fieldExpiresAt, strconv.FormatInt(expiresAt.Unix(), 10),
			fieldFamilyExpiresAt, strconv.FormatInt(familyExpiresAt.Unix(), 10),
		)
		pipe.Expire(ctx, tokenKey(tokenHash), keyTTL)
		pipe.Set(ctx, familyKey(familyID), tokenHash, keyTTL)
		pipe.SAdd(ctx, userSessionsKey(userID), familyID)
		pipe.Expire(ctx, userSessionsKey(userID), familyAbsoluteLifetime)

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("saving refresh token: %w", err)
	}

	return rawToken, nil
}

func (s *RefreshTokenStore) Validate(ctx context.Context, rawToken string) (*domain.RefreshToken, error) {
	tokenHash := domain.HashRefreshToken(rawToken)

	record, err := s.client.HGetAll(ctx, tokenKey(tokenHash)).Result()
	if err != nil {
		return nil, fmt.Errorf("validating refresh token: %w", err)
	}

	if len(record) == 0 {
		return nil, domain.ErrRefreshTokenInvalid
	}

	token, err := toRefreshToken(tokenHash, record)
	if err != nil {
		return nil, fmt.Errorf("validating refresh token: %w", err)
	}

	if err := token.Validate(time.Now()); err != nil {
		return nil, err
	}

	return token, nil
}

func (s *RefreshTokenStore) Rotate(ctx context.Context, rawToken string, ttl time.Duration) (string, string, error) {
	if err := checkTTL(ttl); err != nil {
		return "", "", fmt.Errorf("rotating refresh token: %w", err)
	}

	newRawToken, err := domain.GenerateRefreshToken()
	if err != nil {
		return "", "", fmt.Errorf("rotating refresh token: %w", err)
	}

	oldHash := domain.HashRefreshToken(rawToken)
	newHash := domain.HashRefreshToken(newRawToken)

	now := time.Now()
	keys := []string{tokenKey(oldHash), usedKey(oldHash), tokenKey(newHash)}
	args := []any{
		newHash,
		int64(ttl.Seconds()),
		now.Add(ttl).Unix(),
		now.Unix(),
		refreshFamilyKeyPrefix,
		refreshTokenKeyPrefix,
		int64(rotationGraceWindow.Seconds()),
		now.Add(familyAbsoluteLifetime).Unix(),
		refreshUserSessionsKeyPrefix,
	}

	reply, err := rotateScript.Run(ctx, s.client, keys, args...).StringSlice()
	if err != nil {
		return "", "", fmt.Errorf("rotating refresh token: %w", err)
	}

	if len(reply) == 0 {
		return "", "", fmt.Errorf("rotating refresh token: empty script reply")
	}

	switch reply[0] {
	case rotateOK:
		if len(reply) < 2 {
			return "", "", fmt.Errorf("rotating refresh token: script reply carries no user id")
		}

		return newRawToken, reply[1], nil
	case rotateInvalid:
		return "", "", domain.ErrRefreshTokenInvalid
	case rotateExpired:
		return "", "", domain.ErrRefreshTokenExpired
	case rotateRace:
		logFamilyEvent(
			ctx, zerolog.InfoLevel, "refresh_token_rotation_race", oldHash, reply,
			"Refresh token presented again within the rotation grace window; family left intact",
		)

		return "", "", domain.ErrRefreshTokenInvalid
	case rotateReused:
		logFamilyEvent(
			ctx, zerolog.WarnLevel, "refresh_token_reuse_detected", oldHash, reply,
			"Refresh token reuse detected; token family revoked",
		)

		return "", "", domain.ErrRefreshTokenReused
	default:
		return "", "", fmt.Errorf("rotating refresh token: unknown script outcome %q", reply[0])
	}
}

func (s *RefreshTokenStore) Revoke(ctx context.Context, rawToken string) error {
	tokenHash := domain.HashRefreshToken(rawToken)

	keys := []string{tokenKey(tokenHash), usedKey(tokenHash)}
	args := []any{refreshFamilyKeyPrefix, refreshTokenKeyPrefix, refreshUserSessionsKeyPrefix}

	reply, err := revokeScript.Run(ctx, s.client, keys, args...).StringSlice()
	if err != nil {
		return fmt.Errorf("revoking refresh token: %w", err)
	}

	if len(reply) == 0 {
		return fmt.Errorf("revoking refresh token: empty script reply")
	}

	switch reply[0] {
	case revokeLive, revokeMiss:
		return nil
	case revokeSuperseded:
		logFamilyEvent(
			ctx, zerolog.InfoLevel, "refresh_token_logout_of_superseded_token", tokenHash, reply,
			"Logout presented an already-rotated refresh token; its family was revoked",
		)

		return nil
	default:
		return fmt.Errorf("revoking refresh token: unknown script outcome %q", reply[0])
	}
}

func (s *RefreshTokenStore) RevokeAllForUser(ctx context.Context, userID string) error {
	keys := []string{userSessionsKey(userID)}
	args := []any{refreshFamilyKeyPrefix, refreshTokenKeyPrefix}

	if err := revokeAllScript.Run(ctx, s.client, keys, args...).Err(); err != nil {
		return fmt.Errorf("revoking all refresh tokens for user %s: %w", userID, err)
	}

	return nil
}

func logFamilyEvent(ctx context.Context, level zerolog.Level, name, tokenHash string, reply []string, message string) {
	logger := observability.Logger(ctx)

	event := logger.WithLevel(level).
		Str("event", name).
		Str("token_hash", tokenHash)

	if len(reply) > 1 {
		event = event.Str("family_id", reply[1])
	}

	event.Msg(message)
}

func toRefreshToken(tokenHash string, record map[string]string) (*domain.RefreshToken, error) {
	expiresAt, err := strconv.ParseInt(record[fieldExpiresAt], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing %s of %s: %w", fieldExpiresAt, tokenKey(tokenHash), err)
	}

	return &domain.RefreshToken{
		TokenHash: tokenHash,
		UserID:    record[fieldUserID],
		FamilyID:  record[fieldFamilyID],
		ExpiresAt: time.Unix(expiresAt, 0),
	}, nil
}

func checkTTL(ttl time.Duration) error {
	if ttl < time.Second {
		return fmt.Errorf("ttl must be at least one second, got %s", ttl)
	}

	return nil
}

func newFamilyID() (string, error) {
	buf := make([]byte, familyIDEntropyBytes)

	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating refresh token family id: %w", err)
	}

	return hex.EncodeToString(buf), nil
}

func tokenKey(tokenHash string) string     { return refreshTokenKeyPrefix + tokenHash }
func familyKey(familyID string) string     { return refreshFamilyKeyPrefix + familyID }
func usedKey(tokenHash string) string      { return refreshUsedKeyPrefix + tokenHash }
func userSessionsKey(userID string) string { return refreshUserSessionsKeyPrefix + userID }

var _ domain.RefreshTokenStore = (*RefreshTokenStore)(nil)
