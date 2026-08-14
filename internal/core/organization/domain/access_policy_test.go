package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOrganizationAccessPolicy(t *testing.T) {
	trialEndsAt := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	trialStartsAt := trialEndsAt.Add(-14 * 24 * time.Hour)

	tests := []struct {
		name             string
		status           SubscriptionStatus
		now              time.Time
		wantActive       bool
		wantTrialExpired bool
	}{
		{
			name:             "trialing one second before trial ends",
			status:           Trialing,
			now:              trialEndsAt.Add(-time.Second),
			wantActive:       true,
			wantTrialExpired: false,
		},
		{
			name:             "trialing at the exact trial end instant is expired",
			status:           Trialing,
			now:              trialEndsAt,
			wantActive:       false,
			wantTrialExpired: true,
		},
		{
			name:             "trialing one second after trial ends",
			status:           Trialing,
			now:              trialEndsAt.Add(time.Second),
			wantActive:       false,
			wantTrialExpired: true,
		},
		{
			name:             "trialing well inside the trial window",
			status:           Trialing,
			now:              trialStartsAt.Add(time.Hour),
			wantActive:       true,
			wantTrialExpired: false,
		},
		{
			name:             "active before trial end",
			status:           Active,
			now:              trialEndsAt.Add(-time.Second),
			wantActive:       true,
			wantTrialExpired: false,
		},
		{
			name:             "active at the exact trial end instant",
			status:           Active,
			now:              trialEndsAt,
			wantActive:       true,
			wantTrialExpired: false,
		},
		{
			name:             "active long after trial end is not trial expired",
			status:           Active,
			now:              trialEndsAt.Add(365 * 24 * time.Hour),
			wantActive:       true,
			wantTrialExpired: false,
		},
		{
			name:             "past due before trial end",
			status:           PastDue,
			now:              trialEndsAt.Add(-time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "past due at the exact trial end instant",
			status:           PastDue,
			now:              trialEndsAt,
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "past due after trial end",
			status:           PastDue,
			now:              trialEndsAt.Add(time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "suspended before trial end",
			status:           Suspended,
			now:              trialEndsAt.Add(-time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "suspended at the exact trial end instant",
			status:           Suspended,
			now:              trialEndsAt,
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "suspended after trial end",
			status:           Suspended,
			now:              trialEndsAt.Add(time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "canceled before trial end",
			status:           Canceled,
			now:              trialEndsAt.Add(-time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "canceled at the exact trial end instant",
			status:           Canceled,
			now:              trialEndsAt,
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "canceled after trial end",
			status:           Canceled,
			now:              trialEndsAt.Add(time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "unknown status denies access and is not trial expired",
			status:           SubscriptionStatus("expired"),
			now:              trialEndsAt.Add(-time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
		{
			name:             "empty status denies access and is not trial expired",
			status:           SubscriptionStatus(""),
			now:              trialEndsAt.Add(-time.Second),
			wantActive:       false,
			wantTrialExpired: false,
		},
	}

	policy := OrganizationAccessPolicy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := &Organization{
				Name:               "Acme",
				TrialStartsAt:      trialStartsAt,
				TrialEndsAt:        trialEndsAt,
				SubscriptionStatus: tt.status,
			}

			assert.Equal(t, tt.wantActive, policy.IsOrganizationActive(org, tt.now), "IsOrganizationActive")
			assert.Equal(t, tt.wantTrialExpired, policy.IsTrialExpired(org, tt.now), "IsTrialExpired")
			assert.Equal(
				t,
				policy.IsOrganizationActive(org, tt.now),
				policy.CanPerformBusinessOperations(org, tt.now),
				"CanPerformBusinessOperations must match IsOrganizationActive",
			)
		})
	}
}

func TestOrganizationAccessPolicyTrialingIsActiveOrExpired(t *testing.T) {
	trialEndsAt := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

	offsets := []time.Duration{
		-24 * time.Hour,
		-time.Second,
		-time.Nanosecond,
		0,
		time.Nanosecond,
		time.Second,
		24 * time.Hour,
	}

	policy := OrganizationAccessPolicy{}
	org := &Organization{
		Name:               "Acme",
		TrialStartsAt:      trialEndsAt.Add(-14 * 24 * time.Hour),
		TrialEndsAt:        trialEndsAt,
		SubscriptionStatus: Trialing,
	}

	for _, offset := range offsets {
		t.Run(offset.String(), func(t *testing.T) {
			now := trialEndsAt.Add(offset)

			assert.NotEqual(
				t,
				policy.IsOrganizationActive(org, now),
				policy.IsTrialExpired(org, now),
				"a trialing organization must be either active or trial expired, never both or neither",
			)
		})
	}
}
