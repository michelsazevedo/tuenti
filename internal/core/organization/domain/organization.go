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

	TrialStartsAt      time.Time          `json:"trial_starts_at"`
	TrialEndsAt        time.Time          `json:"trial_ends_at"`
	SubscriptionStatus SubscriptionStatus `json:"subscription_status"`
	PlanID             pgtype.UUID        `json:"plan_id"`
}

const trialDuration = 14 * 24 * time.Hour

var allowedSubscriptionTransitions = map[SubscriptionStatus][]SubscriptionStatus{
	Trialing:  {Active, Suspended},
	Suspended: {Active},
	Active:    {PastDue, Canceled, Suspended},
	PastDue:   {Active, Canceled},
}

func (o *Organization) StartTrial(now time.Time) {
	o.TrialStartsAt = now
	o.TrialEndsAt = now.Add(trialDuration)
	o.SubscriptionStatus = Trialing
}

func (o *Organization) Activate() error {
	return o.transitionTo(Active)
}

func (o *Organization) Suspend() error {
	return o.transitionTo(Suspended)
}

func (o *Organization) MarkPastDue() error {
	return o.transitionTo(PastDue)
}

func (o *Organization) Cancel() error {
	return o.transitionTo(Canceled)
}

func (o *Organization) transitionTo(target SubscriptionStatus) error {
	for _, allowed := range allowedSubscriptionTransitions[o.SubscriptionStatus] {
		if allowed == target {
			o.SubscriptionStatus = target
			return nil
		}
	}

	return ErrInvalidSubscriptionTransition
}
