package persistence

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

const testTokenTTL = 2 * time.Minute

const testUserID = "6f1c0e6a-1f5e-4f3a-9a1f-0f2b3c4d5e6f"

func newTestStore(t *testing.T) (*RefreshTokenStore, *goredis.Client) {
	t.Helper()

	addr := os.Getenv("REDIS_HOST")
	if addr == "" {
		addr = "localhost:6379"
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("redis unavailable at %s: %v (start it with `docker-compose up -d redis`)", addr, err)
	}

	t.Cleanup(func() { _ = client.Close() })

	return NewRefreshTokenStore(client), client
}

func cleanupTokens(t *testing.T, client *goredis.Client, rawTokens ...string) {
	t.Helper()

	ctx := context.Background()

	for _, rawToken := range rawTokens {
		tokenHash := domain.HashRefreshToken(rawToken)
		familyID, _ := client.HGet(ctx, tokenKey(tokenHash), fieldFamilyID).Result()

		t.Cleanup(func() {
			if familyID != "" {
				_ = client.Del(ctx, familyKey(familyID)).Err()
			}

			_ = client.Del(ctx, tokenKey(tokenHash), usedKey(tokenHash)).Err()
		})
	}
}

func expireRecord(t *testing.T, client *goredis.Client, rawToken string) {
	t.Helper()

	backdateField(t, client, rawToken, fieldExpiresAt, time.Now().Add(-time.Minute))
}

func expireFamily(t *testing.T, client *goredis.Client, rawToken string) {
	t.Helper()

	backdateField(t, client, rawToken, fieldFamilyExpiresAt, time.Now().Add(-time.Minute))
}

func backdateField(t *testing.T, client *goredis.Client, rawToken, field string, to time.Time) {
	t.Helper()

	require.NoError(t, client.HSet(
		context.Background(),
		tokenKey(domain.HashRefreshToken(rawToken)),
		field,
		strconv.FormatInt(to.Unix(), 10),
	).Err())
}

func ageTombstone(t *testing.T, client *goredis.Client, rawToken string) {
	t.Helper()

	restampTombstone(t, client, rawToken, time.Now().Add(-rotationGraceWindow-time.Minute))
}

func restampTombstone(t *testing.T, client *goredis.Client, rawToken string, at time.Time) {
	t.Helper()

	ctx := context.Background()
	key := usedKey(domain.HashRefreshToken(rawToken))

	value, err := client.Get(ctx, key).Result()
	require.NoError(t, err, "no tombstone to restamp — was the token rotated?")

	familyID, _, found := strings.Cut(value, ":")
	require.True(t, found, "tombstone %q is not in family_id:rotated_at form", value)

	require.NoError(t, client.Set(
		ctx, key, familyID+":"+strconv.FormatInt(at.Unix(), 10), goredis.KeepTTL,
	).Err())
}

func TestRefreshTokenStoreSave(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	rawToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)
	require.NotEmpty(t, rawToken)

	cleanupTokens(t, client, rawToken)

	token, err := store.Validate(ctx, rawToken)
	require.NoError(t, err)

	assert.Equal(t, testUserID, token.UserID)
	assert.Equal(t, domain.HashRefreshToken(rawToken), token.TokenHash)
	assert.NotEmpty(t, token.FamilyID)
	assert.False(t, token.Revoked)
	assert.WithinDuration(t, time.Now().Add(testTokenTTL), token.ExpiresAt, time.Minute)

	t.Run("only the hash reaches redis", func(t *testing.T) {
		record, err := client.HGetAll(ctx, tokenKey(token.TokenHash)).Result()
		require.NoError(t, err)

		for field, value := range record {
			assert.NotContains(t, value, rawToken, "field %s leaks the raw token", field)
		}

		exists, err := client.Exists(ctx, refreshTokenKeyPrefix+rawToken).Result()
		require.NoError(t, err)
		assert.Zero(t, exists, "a key keyed by the raw token must not exist")
	})

	t.Run("every key is ttl bound", func(t *testing.T) {
		for _, key := range []string{tokenKey(token.TokenHash), familyKey(token.FamilyID)} {
			ttl, err := client.TTL(ctx, key).Result()
			require.NoError(t, err)
			assert.Positive(t, ttl, "key %s has no ttl", key)
		}
	})

	t.Run("family points at the live token", func(t *testing.T) {
		live, err := client.Get(ctx, familyKey(token.FamilyID)).Result()
		require.NoError(t, err)
		assert.Equal(t, token.TokenHash, live)
	})

	t.Run("the family ceiling is minted once, at save", func(t *testing.T) {
		ceiling, err := client.HGet(ctx, tokenKey(token.TokenHash), fieldFamilyExpiresAt).Int64()
		require.NoError(t, err, "a record must carry the deadline its whole family dies at")

		assert.WithinDuration(t, time.Now().Add(familyAbsoluteLifetime), time.Unix(ceiling, 0), time.Minute)

		rotated, _, err := store.Rotate(ctx, rawToken, testTokenTTL)
		require.NoError(t, err)

		cleanupTokens(t, client, rotated)

		carried, err := client.HGet(ctx, tokenKey(domain.HashRefreshToken(rotated)), fieldFamilyExpiresAt).Int64()
		require.NoError(t, err)

		assert.Equal(t, ceiling, carried, "rotation must carry the ceiling forward, never reset it")
	})
}

func TestRefreshTokenStoreRejectsUnrepresentableTTL(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	for _, ttl := range []time.Duration{0, -time.Second, time.Millisecond, 999 * time.Millisecond} {
		saved, err := store.Save(ctx, testUserID, ttl)

		assert.Error(t, err, "Save must reject a ttl of %s", ttl)
		assert.Empty(t, saved)

		rotated, userID, err := store.Rotate(ctx, "irrelevant", ttl)

		assert.Error(t, err, "Rotate must reject a ttl of %s", ttl)
		assert.Empty(t, rotated)
		assert.Empty(t, userID)
		assert.NotErrorIs(t, err, domain.ErrRefreshTokenInvalid)
	}
}

func TestRefreshTokenStoreValidateUnknownToken(t *testing.T) {
	store, _ := newTestStore(t)

	rawToken, err := domain.GenerateRefreshToken()
	require.NoError(t, err)

	token, err := store.Validate(context.Background(), rawToken)

	assert.Nil(t, token)
	assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
}

func TestRefreshTokenStoreValidateExpiredToken(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	rawToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, rawToken)
	expireRecord(t, client, rawToken)

	token, err := store.Validate(ctx, rawToken)

	assert.Nil(t, token)
	assert.ErrorIs(t, err, domain.ErrRefreshTokenExpired)
}

func TestRefreshTokenStoreRotate(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	oldToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, oldToken)

	original, err := store.Validate(ctx, oldToken)
	require.NoError(t, err)

	newToken, rotatedUserID, err := store.Rotate(ctx, oldToken, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, newToken)

	assert.NotEqual(t, oldToken, newToken)

	assert.Equal(t, testUserID, rotatedUserID)

	rotated, err := store.Validate(ctx, newToken)
	require.NoError(t, err)

	assert.Equal(t, testUserID, rotated.UserID)
	assert.Equal(t, original.FamilyID, rotated.FamilyID, "rotation must never change the family")

	t.Run("the presented token is consumed", func(t *testing.T) {
		token, err := store.Validate(ctx, oldToken)

		assert.Nil(t, token)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
	})

	t.Run("the family points at the replacement", func(t *testing.T) {
		live, err := client.Get(ctx, familyKey(rotated.FamilyID)).Result()
		require.NoError(t, err)
		assert.Equal(t, rotated.TokenHash, live)
	})

	t.Run("the tombstone is ttl bound", func(t *testing.T) {
		ttl, err := client.TTL(ctx, usedKey(original.TokenHash)).Result()
		require.NoError(t, err)
		assert.Positive(t, ttl)
	})
}

func TestRefreshTokenStoreRotateUnknownToken(t *testing.T) {
	store, _ := newTestStore(t)

	rawToken, err := domain.GenerateRefreshToken()
	require.NoError(t, err)

	newToken, userID, err := store.Rotate(context.Background(), rawToken, testTokenTTL)

	assert.Empty(t, newToken)
	assert.Empty(t, userID, "a failed rotation must not name a subject")
	assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
}

func TestRefreshTokenStoreRotateExpiredToken(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	rawToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, rawToken)
	expireRecord(t, client, rawToken)

	newToken, userID, err := store.Rotate(ctx, rawToken, testTokenTTL)

	assert.Empty(t, newToken)
	assert.Empty(t, userID, "a failed rotation must not name a subject")
	assert.ErrorIs(t, err, domain.ErrRefreshTokenExpired)

	t.Run("the expired record is dropped, not left to be reused", func(t *testing.T) {
		exists, err := client.Exists(ctx, tokenKey(domain.HashRefreshToken(rawToken))).Result()
		require.NoError(t, err)
		assert.Zero(t, exists)
	})
}

func TestRefreshTokenStoreRotatePastFamilyCeiling(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	rawToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, rawToken)

	_, err = store.Validate(ctx, rawToken)
	require.NoError(t, err, "the token itself must be live — only its family runs out")

	expireFamily(t, client, rawToken)

	newToken, userID, err := store.Rotate(ctx, rawToken, testTokenTTL)

	assert.Empty(t, newToken, "a family past its ceiling must not mint a replacement")
	assert.Empty(t, userID, "a failed rotation must not name a subject")
	assert.ErrorIs(t, err, domain.ErrRefreshTokenExpired)

	t.Run("the record is dropped", func(t *testing.T) {
		exists, err := client.Exists(ctx, tokenKey(domain.HashRefreshToken(rawToken))).Result()
		require.NoError(t, err)
		assert.Zero(t, exists)
	})

	t.Run("no tombstone is left behind to read as reuse", func(t *testing.T) {
		exists, err := client.Exists(ctx, usedKey(domain.HashRefreshToken(rawToken))).Result()
		require.NoError(t, err)
		assert.Zero(t, exists)

		_, _, err = store.Rotate(ctx, rawToken, testTokenTTL)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
	})
}

func TestRefreshTokenStoreRotateCapsTTLAtFamilyCeiling(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	rawToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, rawToken)

	const remaining = 30 * time.Second

	ceiling := time.Now().Add(remaining)
	backdateField(t, client, rawToken, fieldFamilyExpiresAt, ceiling)

	newToken, _, err := store.Rotate(ctx, rawToken, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, newToken)

	replacement, err := store.Validate(ctx, newToken)
	require.NoError(t, err)

	assert.WithinDuration(t, ceiling, replacement.ExpiresAt, time.Second,
		"the replacement must expire with the family, not %s later", testTokenTTL)

	for _, key := range []string{tokenKey(replacement.TokenHash), familyKey(replacement.FamilyID)} {
		ttl, err := client.TTL(ctx, key).Result()
		require.NoError(t, err)

		assert.Positive(t, ttl, "key %s has no ttl", key)
		assert.LessOrEqual(t, ttl, remaining, "key %s outlives the family ceiling", key)
	}
}

func TestRefreshTokenStoreRotateWithinGraceWindow(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	tokenA, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenA)

	family, err := store.Validate(ctx, tokenA)
	require.NoError(t, err)

	tokenB, _, err := store.Rotate(ctx, tokenA, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenB)

	replacement, userID, err := store.Rotate(ctx, tokenA, testTokenTTL)

	assert.Empty(t, replacement, "a losing racer must not mint a replacement")
	assert.Empty(t, userID)
	assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid,
		"a racing client must see a plain invalid token, not a reuse verdict")
	assert.NotErrorIs(t, err, domain.ErrRefreshTokenReused)

	t.Run("the family survives the race", func(t *testing.T) {
		token, err := store.Validate(ctx, tokenB)
		require.NoError(t, err, "the winner's token must still be live")
		assert.Equal(t, family.FamilyID, token.FamilyID)

		live, err := client.Get(ctx, familyKey(family.FamilyID)).Result()
		require.NoError(t, err, "the family pointer must survive a race")
		assert.Equal(t, token.TokenHash, live)
	})

	t.Run("the winner can still rotate", func(t *testing.T) {
		tokenC, userID, err := store.Rotate(ctx, tokenB, testTokenTTL)
		require.NoError(t, err, "a race must not cost the session its next rotation")

		cleanupTokens(t, client, tokenC)
		assert.Equal(t, testUserID, userID)
	})
}

func TestRefreshTokenStoreReuseRevokesFamily(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	tokenA, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenA)

	family, err := store.Validate(ctx, tokenA)
	require.NoError(t, err)

	tokenB, rotatedUserID, err := store.Rotate(ctx, tokenA, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenB)
	require.NotEqual(t, tokenA, tokenB)
	require.Equal(t, testUserID, rotatedUserID)

	_, err = store.Validate(ctx, tokenB)
	require.NoError(t, err, "B must be live before the replay")

	ageTombstone(t, client, tokenA)

	replacement, replayUserID, err := store.Rotate(ctx, tokenA, testTokenTTL)

	assert.Empty(t, replacement, "a replayed token must never mint a replacement")
	assert.Empty(t, replayUserID, "a replay must not tell the attacker whose token it is")
	assert.ErrorIs(t, err, domain.ErrRefreshTokenReused)

	t.Run("the live token is revoked with the family", func(t *testing.T) {
		token, err := store.Validate(ctx, tokenB)

		assert.Nil(t, token)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

		newToken, userID, err := store.Rotate(ctx, tokenB, testTokenTTL)

		assert.Empty(t, newToken)
		assert.Empty(t, userID)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
	})

	t.Run("the family pointer is gone", func(t *testing.T) {
		exists, err := client.Exists(ctx, familyKey(family.FamilyID)).Result()
		require.NoError(t, err)
		assert.Zero(t, exists)
	})

	t.Run("further replays keep reporting reuse", func(t *testing.T) {
		_, _, err := store.Rotate(ctx, tokenA, testTokenTTL)

		assert.ErrorIs(t, err, domain.ErrRefreshTokenReused)
	})
}

func TestRefreshTokenStoreReuseSurvivesTombstoneClockSkew(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	tokenA, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenA)

	family, err := store.Validate(ctx, tokenA)
	require.NoError(t, err)

	tokenB, _, err := store.Rotate(ctx, tokenA, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenB)

	t.Run("the rotation instant comes from redis, not the caller", func(t *testing.T) {
		value, err := client.Get(ctx, usedKey(domain.HashRefreshToken(tokenA))).Result()
		require.NoError(t, err)

		_, stamp, found := strings.Cut(value, ":")
		require.True(t, found, "tombstone %q is not in family_id:rotated_at form", value)

		rotatedAt, err := strconv.ParseInt(stamp, 10, 64)
		require.NoError(t, err, "rotated_at must stay parseable unix seconds")

		redisNow, err := client.Time(ctx).Result()
		require.NoError(t, err)

		assert.WithinDuration(t, redisNow, time.Unix(rotatedAt, 0), time.Minute,
			"the tombstone must be stamped against the clock the grace window is read with")
	})

	restampTombstone(t, client, tokenA, time.Now().Add(time.Hour))

	replacement, replayUserID, err := store.Rotate(ctx, tokenA, testTokenTTL)

	assert.Empty(t, replacement, "a replay must never mint a replacement, whatever the clocks say")
	assert.Empty(t, replayUserID, "a replay must not tell the attacker whose token it is")
	assert.ErrorIs(t, err, domain.ErrRefreshTokenReused,
		"a future-dated tombstone must fail closed as reuse, not be excused as a race")
	assert.NotErrorIs(t, err, domain.ErrRefreshTokenInvalid,
		"filing this as an ordinary invalid token would silence the reuse alert")

	t.Run("the family is revoked, live token included", func(t *testing.T) {
		token, err := store.Validate(ctx, tokenB)

		assert.Nil(t, token)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

		exists, err := client.Exists(ctx, familyKey(family.FamilyID)).Result()
		require.NoError(t, err)
		assert.Zero(t, exists, "the compromised family's pointer must be gone")
	})
}

func TestRefreshTokenStoreRevoke(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	rawToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, rawToken)

	require.NoError(t, store.Revoke(ctx, rawToken))

	token, err := store.Validate(ctx, rawToken)
	assert.Nil(t, token)
	assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

	t.Run("a revoked token cannot be rotated and is not read as an attack", func(t *testing.T) {
		newToken, userID, err := store.Rotate(ctx, rawToken, testTokenTTL)

		assert.Empty(t, newToken)
		assert.Empty(t, userID)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
	})

	t.Run("revoke is idempotent", func(t *testing.T) {
		assert.NoError(t, store.Revoke(ctx, rawToken))
	})

	t.Run("revoking an unknown token is a no-op", func(t *testing.T) {
		unknown, err := domain.GenerateRefreshToken()
		require.NoError(t, err)

		assert.NoError(t, store.Revoke(ctx, unknown))
	})
}

func TestRefreshTokenStoreRevokeEndsTheFamily(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	tokenA, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenA)

	family, err := store.Validate(ctx, tokenA)
	require.NoError(t, err)

	tokenB, _, err := store.Rotate(ctx, tokenA, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, tokenB)

	require.NoError(t, store.Revoke(ctx, tokenB))

	t.Run("the live token is gone", func(t *testing.T) {
		token, err := store.Validate(ctx, tokenB)

		assert.Nil(t, token)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
	})

	t.Run("the family pointer is gone", func(t *testing.T) {
		exists, err := client.Exists(ctx, familyKey(family.FamilyID)).Result()
		require.NoError(t, err)
		assert.Zero(t, exists)
	})
}

func TestRefreshTokenStoreRevokeWithSupersededToken(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	victimToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, victimToken)

	family, err := store.Validate(ctx, victimToken)
	require.NoError(t, err)

	stolenToken, _, err := store.Rotate(ctx, victimToken, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, stolenToken)

	attackerToken, _, err := store.Rotate(ctx, stolenToken, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, attackerToken)

	_, err = store.Validate(ctx, attackerToken)
	require.NoError(t, err, "the attacker's token must be live before the logout")

	require.NoError(t, store.Revoke(ctx, victimToken), "logout must not report token state")

	t.Run("the attacker's live token dies with the family", func(t *testing.T) {
		token, err := store.Validate(ctx, attackerToken)

		assert.Nil(t, token)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

		newToken, userID, err := store.Rotate(ctx, attackerToken, testTokenTTL)

		assert.Empty(t, newToken)
		assert.Empty(t, userID)
		assert.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)
	})

	t.Run("the family pointer is gone", func(t *testing.T) {
		exists, err := client.Exists(ctx, familyKey(family.FamilyID)).Result()
		require.NoError(t, err)
		assert.Zero(t, exists)
	})

	t.Run("logging out twice is still a no-op", func(t *testing.T) {
		assert.NoError(t, store.Revoke(ctx, victimToken))
	})
}

func TestRefreshTokenStoreRevokeSpansOnlyOneFamily(t *testing.T) {
	store, client := newTestStore(t)
	ctx := context.Background()

	phone, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	laptop, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, phone, laptop)

	require.NoError(t, store.Revoke(ctx, phone))

	token, err := store.Validate(ctx, laptop)

	require.NoError(t, err, "revoking one session must not touch the user's others")
	assert.Equal(t, testUserID, token.UserID)
}

func TestRefreshTokenStoreRotateConcurrently(t *testing.T) {
	const callers = 32

	store, client := newTestStore(t)
	ctx := context.Background()

	rawToken, err := store.Save(ctx, testUserID, testTokenTTL)
	require.NoError(t, err)

	cleanupTokens(t, client, rawToken)

	type outcome struct {
		token  string
		userID string
		err    error
	}

	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		outcomes = make(chan outcome, callers)
	)

	for range callers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			token, userID, err := store.Rotate(ctx, rawToken, testTokenTTL)
			outcomes <- outcome{token: token, userID: userID, err: err}
		}()
	}

	close(start)
	wg.Wait()
	close(outcomes)

	var issued []string

	for got := range outcomes {
		if got.err == nil {
			assert.Equal(t, testUserID, got.userID, "the winner must report the family's subject")
			issued = append(issued, got.token)

			continue
		}

		assert.Empty(t, got.token, "a failed rotation must not return a token")
		assert.Empty(t, got.userID, "a failed rotation must not name a subject")
		assert.ErrorIs(t, got.err, domain.ErrRefreshTokenInvalid,
			"a simultaneous loser is a racer, not an attacker")
		assert.NotErrorIs(t, got.err, domain.ErrRefreshTokenReused)
	}

	require.Len(t, issued, 1, "exactly one concurrent rotation may succeed")

	cleanupTokens(t, client, issued[0])

	t.Run("the winner survives the burst", func(t *testing.T) {
		token, err := store.Validate(ctx, issued[0])

		require.NoError(t, err, "31 losing racers must not destroy the session the winner created")
		assert.Equal(t, testUserID, token.UserID)

		next, userID, err := store.Rotate(ctx, issued[0], testTokenTTL)
		require.NoError(t, err, "the surviving token must still be rotatable")

		cleanupTokens(t, client, next)
		assert.Equal(t, testUserID, userID)
	})
}
