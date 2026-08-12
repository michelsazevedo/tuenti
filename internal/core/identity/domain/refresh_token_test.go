package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRefreshTokenIsExpired(t *testing.T) {
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
			token := RefreshToken{ExpiresAt: tt.expiresAt}

			assert.Equal(t, tt.want, token.IsExpired(now))
		})
	}
}

func TestRefreshTokenValidate(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		token   RefreshToken
		wantErr error
	}{
		{
			name:    "live token",
			token:   RefreshToken{ExpiresAt: now.Add(time.Hour)},
			wantErr: nil,
		},
		{
			name:    "expired token",
			token:   RefreshToken{ExpiresAt: now.Add(-time.Hour)},
			wantErr: ErrRefreshTokenExpired,
		},
		{
			name:    "revoked token",
			token:   RefreshToken{ExpiresAt: now.Add(time.Hour), Revoked: true},
			wantErr: ErrRefreshTokenRevoked,
		},
		{
			name:    "revoked takes precedence over expired",
			token:   RefreshToken{ExpiresAt: now.Add(-time.Hour), Revoked: true},
			wantErr: ErrRefreshTokenRevoked,
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
