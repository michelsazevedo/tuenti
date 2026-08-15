package domain

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const invitationHexDigest = `^[0-9a-f]{64}$`

func TestGenerateInvitationToken(t *testing.T) {
	t.Parallel()

	t.Run("carries 256 bits of entropy", func(t *testing.T) {
		t.Parallel()

		token, err := GenerateInvitationToken()
		require.NoError(t, err)

		require.NotEmpty(t, token)

		raw, err := base64.RawURLEncoding.DecodeString(token)
		require.NoError(t, err, "token must be unpadded URL-safe base64")
		assert.Len(t, raw, 32, "invitation token must carry 256 bits of randomness")
	})

	t.Run("is URL safe", func(t *testing.T) {
		t.Parallel()

		token, err := GenerateInvitationToken()
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
			token, err := GenerateInvitationToken()
			require.NoError(t, err)

			_, duplicate := seen[token]
			require.False(t, duplicate, "generated a duplicate invitation token")

			seen[token] = struct{}{}
		}

		assert.Len(t, seen, samples)
	})
}

func TestHashInvitationToken(t *testing.T) {
	t.Parallel()

	t.Run("is deterministic", func(t *testing.T) {
		t.Parallel()

		token, err := GenerateInvitationToken()
		require.NoError(t, err)

		assert.Equal(t, HashInvitationToken(token), HashInvitationToken(token))
	})

	t.Run("produces a 64 character hex digest", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"generated token": mustGenerateInvitationToken(t),
			"empty token":     "",
			"unicode token":   "tökén-🔑",
		}

		for name, token := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				digest := HashInvitationToken(token)

				assert.Len(t, digest, 64)
				assert.Regexp(t, invitationHexDigest, digest, "digest must be lowercase hex")
			})
		}
	})

	t.Run("matches the SHA-256 reference vector", func(t *testing.T) {
		t.Parallel()

		assert.Equal(
			t,
			"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			HashInvitationToken("abc"),
		)
	})

	t.Run("distinct tokens produce distinct digests", func(t *testing.T) {
		t.Parallel()

		first, err := GenerateInvitationToken()
		require.NoError(t, err)

		second, err := GenerateInvitationToken()
		require.NoError(t, err)

		require.NotEqual(t, first, second)
		assert.NotEqual(t, HashInvitationToken(first), HashInvitationToken(second))
	})

	t.Run("never leaks the raw token", func(t *testing.T) {
		t.Parallel()

		token, err := GenerateInvitationToken()
		require.NoError(t, err)

		digest := HashInvitationToken(token)

		assert.NotEqual(t, token, digest, "digest must not be the identity function")
		assert.NotContains(t, digest, token, "digest must not embed the raw token")
		assert.False(t, strings.HasPrefix(digest, token[:8]), "digest must not share a prefix with the raw token")
	})
}

func mustGenerateInvitationToken(t *testing.T) string {
	t.Helper()

	token, err := GenerateInvitationToken()
	require.NoError(t, err)

	return token
}
