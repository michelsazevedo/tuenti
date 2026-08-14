package domain

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type PasswordResetToken struct {
	Id          pgtype.UUID
	UserID      pgtype.UUID
	TokenDigest string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CreatedAt   time.Time
}

func (t *PasswordResetToken) IsExpired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

func (t *PasswordResetToken) IsUsed() bool {
	return t.UsedAt != nil
}

func (t *PasswordResetToken) Validate(now time.Time) error {
	if t.IsUsed() {
		return ErrPasswordResetTokenUsed
	}

	if t.IsExpired(now) {
		return ErrPasswordResetTokenExpired
	}

	return nil
}
