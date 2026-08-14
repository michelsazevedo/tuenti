package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

const testSecret = "s3cr3t-signing-key"

var errStoreUnavailable = errors.New("redis: connection refused")

type fakeRefreshTokenStore struct {
	rotatedToken  string
	rotatedUserID string
	rotateErr     error

	rotateCalls       int
	presentedToRotate string
	rotateTTL         time.Duration
}

func (f *fakeRefreshTokenStore) Save(context.Context, string, time.Duration) (string, error) {
	panic("Save must not be called by the refresh use case")
}

func (f *fakeRefreshTokenStore) Revoke(context.Context, string) error {
	panic("Revoke must not be called by the refresh use case")
}

func (f *fakeRefreshTokenStore) RevokeAllForUser(context.Context, string) error {
	panic("RevokeAllForUser must not be called by the refresh use case")
}

func (f *fakeRefreshTokenStore) Validate(context.Context, string) (*domain.RefreshToken, error) {
	panic("Validate must not gate the refresh path: it cannot see reuse, so it would suppress family revocation")
}

func (f *fakeRefreshTokenStore) Rotate(_ context.Context, rawToken string, ttl time.Duration) (string, string, error) {
	f.rotateCalls++
	f.presentedToRotate = rawToken
	f.rotateTTL = ttl

	if f.rotateErr != nil {
		return "", "", f.rotateErr
	}

	return f.rotatedToken, f.rotatedUserID, nil
}

func newTestRefresh(store domain.RefreshTokenStore) RefreshUseCase {
	return NewRefresh(store, &config.Config{Settings: config.Settings{Secret: testSecret}})
}

func parseAccessToken(t *testing.T, accessToken string) *jwt.RegisteredClaims {
	t.Helper()

	claims := &jwt.RegisteredClaims{}

	parsed, err := jwt.ParseWithClaims(accessToken, claims, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	require.NoError(t, err)
	require.True(t, parsed.Valid)

	return claims
}

func TestRefreshSuccess(t *testing.T) {
	store := &fakeRefreshTokenStore{
		rotatedToken:  "the-replacement-raw-token",
		rotatedUserID: "6f1c9a2e-1f4b-4d3a-9c11-2b8a7d5e0f33",
	}

	pair, err := newTestRefresh(store).Refresh(context.Background(), "the-presented-raw-token")

	require.NoError(t, err)
	require.NotNil(t, pair)

	assert.Equal(t, "the-replacement-raw-token", pair.RefreshToken)
	assert.Equal(t, int64(accessTokenTTL.Seconds()), pair.ExpiresIn)
	assert.NotEmpty(t, pair.AccessToken)

	claims := parseAccessToken(t, pair.AccessToken)
	assert.Equal(t, "6f1c9a2e-1f4b-4d3a-9c11-2b8a7d5e0f33", claims.Subject)
	assert.WithinDuration(t, time.Now().Add(accessTokenTTL), claims.ExpiresAt.Time, time.Minute)
	assert.NotNil(t, claims.IssuedAt)

	assert.Equal(t, 1, store.rotateCalls)
	assert.Equal(t, "the-presented-raw-token", store.presentedToRotate)
	assert.Equal(t, refreshTokenTTL, store.rotateTTL)

	assert.NotEqual(t, "the-presented-raw-token", pair.RefreshToken)
}

func TestRefreshPropagatesRotateErrors(t *testing.T) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{name: "replayed token, family revoked", wantErr: domain.ErrRefreshTokenReused},
		{name: "unknown, evicted or revoked token", wantErr: domain.ErrRefreshTokenInvalid},
		{name: "expired token", wantErr: domain.ErrRefreshTokenExpired},
		{name: "explicitly revoked token", wantErr: domain.ErrRefreshTokenRevoked},
		{name: "infrastructure failure", wantErr: errStoreUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeRefreshTokenStore{rotateErr: tt.wantErr}

			pair, err := newTestRefresh(store).Refresh(context.Background(), "the-presented-raw-token")

			assert.Nil(t, pair, "no token pair may be returned when rotation fails")
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, 1, store.rotateCalls, "the token must be presented to Rotate, not pre-screened")
		})
	}
}
