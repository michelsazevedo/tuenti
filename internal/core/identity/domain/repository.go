package domain

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByID(ctx context.Context, id pgtype.UUID) (*User, error)
	UpdatePasswordDigest(ctx context.Context, userID pgtype.UUID, passwordDigest string) error
	MarkConfirmed(ctx context.Context, id pgtype.UUID, at time.Time) error
}
