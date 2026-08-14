package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubscriptionStatusValid(t *testing.T) {
	tests := []struct {
		name   string
		status SubscriptionStatus
		want   bool
	}{
		{name: "trialing", status: Trialing, want: true},
		{name: "active", status: Active, want: true},
		{name: "past due", status: PastDue, want: true},
		{name: "suspended", status: Suspended, want: true},
		{name: "canceled", status: Canceled, want: true},
		{name: "unknown status", status: SubscriptionStatus("expired"), want: false},
		{name: "empty status", status: SubscriptionStatus(""), want: false},
		{name: "wrong case", status: SubscriptionStatus("Active"), want: false},
		{name: "wrong separator", status: SubscriptionStatus("past-due"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Valid())
		})
	}
}
