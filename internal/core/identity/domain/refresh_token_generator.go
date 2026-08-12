package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const refreshTokenEntropyBytes = 32

func GenerateRefreshToken() (string, error) {
	buf := make([]byte, refreshTokenEntropyBytes)

	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating refresh token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashRefreshToken(token string) string {
	digest := sha256.Sum256([]byte(token))

	return hex.EncodeToString(digest[:])
}
