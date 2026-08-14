package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

type revokeSpyStore struct {
	revokedToken string
	revokeCalls  int
	revokeErr    error
}

func (s *revokeSpyStore) Save(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	return "", errors.New("unexpected call to Save")
}

func (s *revokeSpyStore) Validate(ctx context.Context, rawToken string) (*domain.RefreshToken, error) {
	return nil, errors.New("unexpected call to Validate")
}

func (s *revokeSpyStore) Rotate(ctx context.Context, rawToken string, ttl time.Duration) (string, string, error) {
	return "", "", errors.New("unexpected call to Rotate")
}

func (s *revokeSpyStore) RevokeAllForUser(ctx context.Context, userID string) error {
	return errors.New("unexpected call to RevokeAllForUser")
}

func (s *revokeSpyStore) Revoke(ctx context.Context, rawToken string) error {
	s.revokeCalls++
	s.revokedToken = rawToken

	return s.revokeErr
}

func TestLogout(t *testing.T) {
	infraErr := errors.New("redis: connection refused")

	tests := []struct {
		name         string
		refreshToken string
		revokeErr    error
		wantErr      error
	}{
		{
			name:         "revokes the presented token",
			refreshToken: "raw-refresh-token",
		},
		{
			name:         "unknown token still succeeds",
			refreshToken: "never-issued-token",
		},
		{
			name:         "infrastructure failure propagates unchanged",
			refreshToken: "raw-refresh-token",
			revokeErr:    infraErr,
			wantErr:      infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &revokeSpyStore{revokeErr: tt.revokeErr}

			err := NewLogout(store).Logout(context.Background(), tt.refreshToken)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, 1, store.revokeCalls)
			assert.Equal(t, tt.refreshToken, store.revokedToken)
		})
	}
}
