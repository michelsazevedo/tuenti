package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPasswordResetTokenIsExpired(t *testing.T) {
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
			token := PasswordResetToken{ExpiresAt: tt.expiresAt}

			assert.Equal(t, tt.want, token.IsExpired(now))
		})
	}
}

func TestPasswordResetTokenIsUsed(t *testing.T) {
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
			token := PasswordResetToken{UsedAt: tt.usedAt}

			assert.Equal(t, tt.want, token.IsUsed())
		})
	}
}

func TestPasswordResetTokenValidate(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	usedAt := now.Add(-time.Minute)

	tests := []struct {
		name    string
		token   PasswordResetToken
		wantErr error
	}{
		{
			name:    "live token",
			token:   PasswordResetToken{ExpiresAt: now.Add(time.Hour)},
			wantErr: nil,
		},
		{
			name:    "expired token",
			token:   PasswordResetToken{ExpiresAt: now.Add(-time.Hour)},
			wantErr: ErrPasswordResetTokenExpired,
		},
		{
			name:    "used token",
			token:   PasswordResetToken{ExpiresAt: now.Add(time.Hour), UsedAt: &usedAt},
			wantErr: ErrPasswordResetTokenUsed,
		},
		{
			name:    "used takes precedence over expired",
			token:   PasswordResetToken{ExpiresAt: now.Add(-time.Hour), UsedAt: &usedAt},
			wantErr: ErrPasswordResetTokenUsed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.token.Validate(now)

			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}

			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
