package domain

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type EmailConfirmationToken struct {
	Id          pgtype.UUID
	UserID      pgtype.UUID
	TokenDigest string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CreatedAt   time.Time
}

func (t *EmailConfirmationToken) IsExpired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

func (t *EmailConfirmationToken) IsUsed() bool {
	return t.UsedAt != nil
}
