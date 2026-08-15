package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const invitationTokenEntropyBytes = 32

func GenerateInvitationToken() (string, error) {
	buf := make([]byte, invitationTokenEntropyBytes)

	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating invitation token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashInvitationToken(token string) string {
	digest := sha256.Sum256([]byte(token))

	return hex.EncodeToString(digest[:])
}
