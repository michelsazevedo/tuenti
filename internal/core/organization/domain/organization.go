package domain

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type Organization struct {
	Id        pgtype.UUID `json:"id"`
	Name      string      `json:"name" validate:"required"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}
