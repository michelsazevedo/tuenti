package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrganizationStartTrial(t *testing.T) {
	tests := []struct {
		name string
		org  Organization
		now  time.Time
	}{
		{
			name: "brand new organization",
			org:  Organization{Name: "Acme"},
			now:  time.Date(2026, time.August, 13, 10, 30, 0, 0, time.UTC),
		},
		{
			name: "non UTC location is preserved",
			org:  Organization{Name: "Acme"},
			now:  time.Date(2026, time.August, 13, 10, 30, 0, 0, time.FixedZone("BRT", -3*60*60)),
		},
		{
			name: "overwrites a previous trial window",
			org: Organization{
				Name:               "Acme",
				TrialStartsAt:      time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
				TrialEndsAt:        time.Date(2025, time.January, 15, 0, 0, 0, 0, time.UTC),
				SubscriptionStatus: Canceled,
			},
			now: time.Date(2026, time.August, 13, 10, 30, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := tt.org

			org.StartTrial(tt.now)

			assert.Equal(t, tt.now, org.TrialStartsAt)
			assert.Equal(t, tt.now.Add(14*24*time.Hour), org.TrialEndsAt)
			assert.Equal(t, Trialing, org.SubscriptionStatus)
			assert.Equal(t, 14*24*time.Hour, org.TrialEndsAt.Sub(org.TrialStartsAt))
		})
	}
}

func TestOrganizationValidSubscriptionTransitions(t *testing.T) {
	tests := []struct {
		name       string
		from       SubscriptionStatus
		transition func(*Organization) error
		want       SubscriptionStatus
	}{
		{name: "trialing to active", from: Trialing, transition: (*Organization).Activate, want: Active},
		{name: "trialing to suspended", from: Trialing, transition: (*Organization).Suspend, want: Suspended},
		{name: "suspended to active", from: Suspended, transition: (*Organization).Activate, want: Active},
		{name: "active to past due", from: Active, transition: (*Organization).MarkPastDue, want: PastDue},
		{name: "active to canceled", from: Active, transition: (*Organization).Cancel, want: Canceled},
		{name: "active to suspended", from: Active, transition: (*Organization).Suspend, want: Suspended},
		{name: "past due to active", from: PastDue, transition: (*Organization).Activate, want: Active},
		{name: "past due to canceled", from: PastDue, transition: (*Organization).Cancel, want: Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := Organization{Name: "Acme", SubscriptionStatus: tt.from}

			err := tt.transition(&org)

			require.NoError(t, err)
			assert.Equal(t, tt.want, org.SubscriptionStatus)
		})
	}
}

func TestOrganizationInvalidSubscriptionTransitions(t *testing.T) {
	tests := []struct {
		name       string
		from       SubscriptionStatus
		transition func(*Organization) error
	}{
		{name: "trialing cannot go past due", from: Trialing, transition: (*Organization).MarkPastDue},
		{name: "trialing cannot be canceled", from: Trialing, transition: (*Organization).Cancel},
		{name: "suspended cannot be suspended again", from: Suspended, transition: (*Organization).Suspend},
		{name: "suspended cannot go past due", from: Suspended, transition: (*Organization).MarkPastDue},
		{name: "active cannot be activated again", from: Active, transition: (*Organization).Activate},
		{name: "past due cannot go past due again", from: PastDue, transition: (*Organization).MarkPastDue},
		{name: "past due cannot be suspended", from: PastDue, transition: (*Organization).Suspend},
		{name: "canceled cannot be reactivated", from: Canceled, transition: (*Organization).Activate},
		{name: "canceled cannot be suspended", from: Canceled, transition: (*Organization).Suspend},
		{name: "canceled cannot be canceled again", from: Canceled, transition: (*Organization).Cancel},
		{name: "unset status cannot be activated", from: SubscriptionStatus(""), transition: (*Organization).Activate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := Organization{Name: "Acme", SubscriptionStatus: tt.from}

			err := tt.transition(&org)

			require.ErrorIs(t, err, ErrInvalidSubscriptionTransition)
			assert.Equal(t, tt.from, org.SubscriptionStatus)
		})
	}
}
