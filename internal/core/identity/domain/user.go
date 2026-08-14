package domain

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type User struct {
	Id             pgtype.UUID `json:"id"`
	Name           string      `json:"name" validate:"required"`
	Email          string      `json:"email" validate:"required,email"`
	Password       string      `json:"-" validate:"required,min=8"`
	PasswordDigest string      `json:"-" validate:"required"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	DeletedAt      time.Time   `json:"-"`
	ConfirmedAt    *time.Time  `json:"-"`
}

func (u *User) GetAttributes() []interface{} {
	return []interface{}{u.Name, u.Email, u.PasswordDigest}
}

func (u *User) IsConfirmed() bool {
	return u.ConfirmedAt != nil
}
