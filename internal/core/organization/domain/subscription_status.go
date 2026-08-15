package domain

type SubscriptionStatus string

const (
	Trialing  SubscriptionStatus = "trialing"
	Active    SubscriptionStatus = "active"
	PastDue   SubscriptionStatus = "past_due"
	Suspended SubscriptionStatus = "suspended"
	Canceled  SubscriptionStatus = "canceled"
)

func (s SubscriptionStatus) Valid() bool {
	switch s {
	case Trialing, Active, PastDue, Suspended, Canceled:
		return true
	default:
		return false
	}
}
