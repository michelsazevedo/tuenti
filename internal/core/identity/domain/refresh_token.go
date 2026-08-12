package domain

import "time"

type RefreshToken struct {
	TokenHash string
	UserID    string
	FamilyID  string
	ExpiresAt time.Time
	Revoked   bool
}

func (t *RefreshToken) IsExpired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

func (t *RefreshToken) Validate(now time.Time) error {
	if t.Revoked {
		return ErrRefreshTokenRevoked
	}

	if t.IsExpired(now) {
		return ErrRefreshTokenExpired
	}

	return nil
}
