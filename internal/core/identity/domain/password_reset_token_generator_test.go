package domain_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

func TestGeneratePasswordResetToken(t *testing.T) {
	t.Parallel()

	t.Run("carries 256 bits of entropy", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GeneratePasswordResetToken()
		require.NoError(t, err)

		require.NotEmpty(t, token)

		raw, err := base64.RawURLEncoding.DecodeString(token)
		require.NoError(t, err, "token must be unpadded URL-safe base64")
		assert.Len(t, raw, 32, "password reset token must carry 256 bits of randomness")
	})

	t.Run("is URL safe", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GeneratePasswordResetToken()
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
			token, err := domain.GeneratePasswordResetToken()
			require.NoError(t, err)

			_, duplicate := seen[token]
			require.False(t, duplicate, "generated a duplicate password reset token")

			seen[token] = struct{}{}
		}

		assert.Len(t, seen, samples)
	})
}

func TestHashPasswordResetToken(t *testing.T) {
	t.Parallel()

	t.Run("is deterministic", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GeneratePasswordResetToken()
		require.NoError(t, err)

		assert.Equal(t, domain.HashPasswordResetToken(token), domain.HashPasswordResetToken(token))
	})

	t.Run("produces a 64 character hex digest", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"generated token": mustGeneratePasswordResetToken(t),
			"empty token":     "",
			"unicode token":   "tökén-🔑",
		}

		for name, token := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				digest := domain.HashPasswordResetToken(token)

				assert.Len(t, digest, 64)
				assert.Regexp(t, hexDigest, digest, "digest must be lowercase hex")
			})
		}
	})

	t.Run("matches the SHA-256 reference vector", func(t *testing.T) {
		t.Parallel()

		assert.Equal(
			t,
			"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			domain.HashPasswordResetToken("abc"),
		)
	})

	t.Run("distinct tokens produce distinct digests", func(t *testing.T) {
		t.Parallel()

		first, err := domain.GeneratePasswordResetToken()
		require.NoError(t, err)

		second, err := domain.GeneratePasswordResetToken()
		require.NoError(t, err)

		require.NotEqual(t, first, second)
		assert.NotEqual(t, domain.HashPasswordResetToken(first), domain.HashPasswordResetToken(second))
	})

	t.Run("never leaks the raw token", func(t *testing.T) {
		t.Parallel()

		token, err := domain.GeneratePasswordResetToken()
		require.NoError(t, err)

		digest := domain.HashPasswordResetToken(token)

		assert.NotEqual(t, token, digest, "digest must not be the identity function")
		assert.NotContains(t, digest, token, "digest must not embed the raw token")
		assert.False(t, strings.HasPrefix(digest, token[:8]), "digest must not share a prefix with the raw token")
	})
}

func mustGeneratePasswordResetToken(t *testing.T) string {
	t.Helper()

	token, err := domain.GeneratePasswordResetToken()
	require.NoError(t, err)

	return token
}
