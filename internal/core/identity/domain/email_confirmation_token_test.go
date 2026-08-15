package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEmailConfirmationTokenIsExpired(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{name: "expiry in the future", expiresAt: now.Add(time.Minute), want: false},
		{name: "expiry exactly now", expiresAt: now, want: true},
		{name: "expiry in the past", expiresAt: now.Add(-time.Minute), want: true},
		{name: "zero expiry", expiresAt: time.Time{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := EmailConfirmationToken{ExpiresAt: tt.expiresAt}

			assert.Equal(t, tt.want, token.IsExpired(now))
		})
	}
}

func TestEmailConfirmationTokenIsUsed(t *testing.T) {
	usedAt := time.Date(2026, time.August, 10, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		usedAt *time.Time
		want   bool
	}{
		{name: "never used", usedAt: nil, want: false},
		{name: "used", usedAt: &usedAt, want: true},
		{name: "used at the zero time", usedAt: &time.Time{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := EmailConfirmationToken{UsedAt: tt.usedAt}

			assert.Equal(t, tt.want, token.IsUsed())
		})
	}
}

func TestEmailConfirmationTokenUsedAndExpiredAreIndependent(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	usedAt := now.Add(-time.Minute)

	token := EmailConfirmationToken{ExpiresAt: now.Add(-time.Hour), UsedAt: &usedAt}

	assert.True(t, token.IsUsed())
	assert.True(t, token.IsExpired(now))
}
