package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInvitationIsExpired(t *testing.T) {
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)

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
			invitation := Invitation{ExpiresAt: tt.expiresAt}

			assert.Equal(t, tt.want, invitation.IsExpired(now))
		})
	}
}

func TestInvitationIsRevoked(t *testing.T) {
	revokedAt := time.Date(2026, time.August, 14, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		revokedAt *time.Time
		want      bool
	}{
		{name: "never revoked", revokedAt: nil, want: false},
		{name: "revoked", revokedAt: &revokedAt, want: true},
		{name: "revoked at the zero time", revokedAt: &time.Time{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invitation := Invitation{RevokedAt: tt.revokedAt}

			assert.Equal(t, tt.want, invitation.IsRevoked())
		})
	}
}

func TestInvitationIsAccepted(t *testing.T) {
	acceptedAt := time.Date(2026, time.August, 14, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		acceptedAt *time.Time
		want       bool
	}{
		{name: "never accepted", acceptedAt: nil, want: false},
		{name: "accepted", acceptedAt: &acceptedAt, want: true},
		{name: "accepted at the zero time", acceptedAt: &time.Time{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invitation := Invitation{AcceptedAt: tt.acceptedAt}

			assert.Equal(t, tt.want, invitation.IsAccepted())
		})
	}
}

func TestInvitationValidate(t *testing.T) {
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)

	tests := []struct {
		name       string
		invitation Invitation
		wantErr    error
	}{
		{
			name:       "pending invitation",
			invitation: Invitation{ExpiresAt: now.Add(time.Hour)},
			wantErr:    nil,
		},
		{
			name:       "expired invitation",
			invitation: Invitation{ExpiresAt: now.Add(-time.Hour)},
			wantErr:    ErrInvitationExpired,
		},
		{
			name:       "revoked invitation",
			invitation: Invitation{ExpiresAt: now.Add(time.Hour), RevokedAt: &past},
			wantErr:    ErrInvitationRevoked,
		},
		{
			name:       "accepted invitation",
			invitation: Invitation{ExpiresAt: now.Add(time.Hour), AcceptedAt: &past},
			wantErr:    ErrInvitationAlreadyAccepted,
		},
		{
			name:       "accepted takes precedence over revoked",
			invitation: Invitation{ExpiresAt: now.Add(time.Hour), AcceptedAt: &past, RevokedAt: &past},
			wantErr:    ErrInvitationAlreadyAccepted,
		},
		{
			name:       "accepted takes precedence over expired",
			invitation: Invitation{ExpiresAt: now.Add(-time.Hour), AcceptedAt: &past},
			wantErr:    ErrInvitationAlreadyAccepted,
		},
		{
			name:       "revoked takes precedence over expired",
			invitation: Invitation{ExpiresAt: now.Add(-time.Hour), RevokedAt: &past},
			wantErr:    ErrInvitationRevoked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.invitation.Validate(now)

			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}

			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
