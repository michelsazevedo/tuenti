package domain_test

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestGenerateRefreshToken(t *testing.T) {
	t.Parallel()

	t.Run("carries 256 bits of entropy", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GenerateRefreshToken()
		require.NoError(t, err)

		raw, err := base64.RawURLEncoding.DecodeString(token)
		require.NoError(t, err, "token must be unpadded URL-safe base64")
		assert.Len(t, raw, 32, "refresh token must carry 256 bits of randomness")
	})

	t.Run("is URL safe", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GenerateRefreshToken()
		require.NoError(t, err)

		assert.NotContains(t, token, "+")
		assert.NotContains(t, token, "/")
		assert.NotContains(t, token, "=")
	})

	t.Run("does not collide across calls", func(t *testing.T) {
		t.Parallel()

		const samples = 10_000

		seen := make(map[string]struct{}, samples)
		for range samples {
			token, err := domain.GenerateRefreshToken()
			require.NoError(t, err)

			_, duplicate := seen[token]
			require.False(t, duplicate, "generated a duplicate refresh token")

			seen[token] = struct{}{}
		}

		assert.Len(t, seen, samples)
	})
}

func TestHashRefreshToken(t *testing.T) {
	t.Parallel()

	t.Run("is deterministic", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GenerateRefreshToken()
		require.NoError(t, err)

		assert.Equal(t, domain.HashRefreshToken(token), domain.HashRefreshToken(token))
	})

	t.Run("produces a 64 character hex digest usable as a redis key", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"generated token": mustGenerate(t),
			"empty token":     "",
			"unicode token":   "tökén-🔑",
		}

		for name, token := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				hash := domain.HashRefreshToken(token)

				assert.Len(t, hash, 64)
				assert.Regexp(t, hexDigest, hash, "hash must be lowercase hex, safe for a redis key")
				assert.NotContains(t, hash, ":", "hash must not break the redis key namespace separator")
			})
		}
	})

	t.Run("matches the SHA-256 reference vector", func(t *testing.T) {
		t.Parallel()

		assert.Equal(
			t,
			"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			domain.HashRefreshToken("abc"),
		)
	})

	t.Run("distinct tokens produce distinct hashes", func(t *testing.T) {
		t.Parallel()

		first, err := domain.GenerateRefreshToken()
		require.NoError(t, err)

		second, err := domain.GenerateRefreshToken()
		require.NoError(t, err)

		require.NotEqual(t, first, second)
		assert.NotEqual(t, domain.HashRefreshToken(first), domain.HashRefreshToken(second))
	})

	t.Run("never leaks the raw token", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GenerateRefreshToken()
		require.NoError(t, err)

		hash := domain.HashRefreshToken(token)

		assert.NotEqual(t, token, hash, "hash must not be the identity function")
		assert.NotContains(t, hash, token, "hash must not embed the raw token")
		assert.False(t, strings.HasPrefix(hash, token[:8]), "hash must not share a prefix with the raw token")
	})
}

func mustGenerate(t *testing.T) string {
	t.Helper()

	token, err := domain.GenerateRefreshToken()
	require.NoError(t, err)

	return token
}
