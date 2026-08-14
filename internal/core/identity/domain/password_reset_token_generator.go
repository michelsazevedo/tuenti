package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const passwordResetTokenEntropyBytes = 32

func GeneratePasswordResetToken() (string, error) {
	buf := make([]byte, passwordResetTokenEntropyBytes)

	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating password reset token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashPasswordResetToken(token string) string {
	digest := sha256.Sum256([]byte(token))

	return hex.EncodeToString(digest[:])
}
