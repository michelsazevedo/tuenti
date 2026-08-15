package domain

import "time"

type OrganizationAccessPolicy struct{}

func (p OrganizationAccessPolicy) IsOrganizationActive(org *Organization, now time.Time) bool {
	switch org.SubscriptionStatus {
	case Active:
		return true
	case Trialing:
		return now.Before(org.TrialEndsAt)
	default:
		return false
	}
}

func (p OrganizationAccessPolicy) IsTrialExpired(org *Organization, now time.Time) bool {
	return org.SubscriptionStatus == Trialing && !now.Before(org.TrialEndsAt)
}

func (p OrganizationAccessPolicy) CanPerformBusinessOperations(org *Organization, now time.Time) bool {
	return p.IsOrganizationActive(org, now)
}
