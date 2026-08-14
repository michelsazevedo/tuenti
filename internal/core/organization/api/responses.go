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

	TrialStartsAt      time.Time                 `json:"trial_starts_at"`
	TrialEndsAt        time.Time                 `json:"trial_ends_at"`
	SubscriptionStatus domain.SubscriptionStatus `json:"subscription_status"`
	PlanID             pgtype.UUID               `json:"plan_id"`
}

func NewOrganizationResponse(org *domain.Organization) OrganizationResponse {
	return OrganizationResponse{
		Id:        org.Id,
		Name:      org.Name,
		CreatedAt: org.CreatedAt,

		TrialStartsAt:      org.TrialStartsAt,
		TrialEndsAt:        org.TrialEndsAt,
		SubscriptionStatus: org.SubscriptionStatus,
		PlanID:             org.PlanID,
	}
}
