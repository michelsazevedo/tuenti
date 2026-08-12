package api

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type OrganizationResponse struct {
	Id        pgtype.UUID `json:"id"`
	Name      string      `json:"name"`
	CreatedAt time.Time   `json:"created_at"`
}

func NewOrganizationResponse(org *domain.Organization) OrganizationResponse {
	return OrganizationResponse{
		Id:        org.Id,
		Name:      org.Name,
		CreatedAt: org.CreatedAt,
	}
}
