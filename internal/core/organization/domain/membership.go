package domain

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type Membership struct {
	Id             pgtype.UUID `json:"id"`
	OrganizationId pgtype.UUID `json:"organization_id"`
	UserId         pgtype.UUID `json:"user_id"`
	Role           Role        `json:"role" validate:"required,oneof=owner admin member"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}
