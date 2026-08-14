package domain

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	FindByEmail(ctx context.Context, email string) (*User, error)
	UpdatePasswordDigest(ctx context.Context, userID pgtype.UUID, passwordDigest string) error
}
