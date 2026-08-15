package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const emailConfirmationTokenEntropyBytes = 32

func GenerateEmailConfirmationToken() (string, error) {
	buf := make([]byte, emailConfirmationTokenEntropyBytes)

	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating email confirmation token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashEmailConfirmationToken(token string) string {
	digest := sha256.Sum256([]byte(token))

	return hex.EncodeToString(digest[:])
}
