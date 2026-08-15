package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

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

func newTestRefresh(store domain.RefreshTokenStore, memberships orgdomain.MembershipRepository) RefreshUseCase {
	return NewRefresh(store, memberships, &config.Config{
		Settings: config.Settings{Secret: testSecret, Environment: testEnvironment},
	})
}

func TestRefreshSuccess(t *testing.T) {
	store := &fakeRefreshTokenStore{
		rotatedToken:  "the-replacement-raw-token",
		rotatedUserID: testUserID,
	}
	memberships := newFakeMemberships()

	pair, err := newTestRefresh(store, memberships).Refresh(context.Background(), "the-presented-raw-token")

	require.NoError(t, err)
	require.NotNil(t, pair)

	assert.Equal(t, "the-replacement-raw-token", pair.RefreshToken)
	assert.Equal(t, int64(accessTokenTTL.Seconds()), pair.ExpiresIn)
	assert.NotEmpty(t, pair.AccessToken)

	claims := parseAccessToken(t, pair.AccessToken)
	assert.Equal(t, testUserID, claims.Subject)
	assert.Equal(t, testOrganizationID, claims.OrganizationID,
		"the refreshed token must carry the organization the user belongs to")
	assert.WithinDuration(t, time.Now().Add(accessTokenTTL), claims.ExpiresAt.Time, time.Minute)
	assert.NotNil(t, claims.IssuedAt)

	assert.Equal(t, 1, memberships.findCalls)
	assert.Equal(t, mustUUID(testUserID), memberships.lookedUpUser,
		"the subject returned by Rotate must be parsed into the uuid used for the lookup")

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
			memberships := newFakeMemberships()

			pair, err := newTestRefresh(store, memberships).Refresh(context.Background(), "the-presented-raw-token")

			assert.Nil(t, pair, "no token pair may be returned when rotation fails")
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, 1, store.rotateCalls, "the token must be presented to Rotate, not pre-screened")
			assert.Zero(t, memberships.findCalls, "a rejected rotation must not reach the database")
		})
	}
}

func TestRefreshRejectsNonUUIDSubject(t *testing.T) {
	store := &fakeRefreshTokenStore{
		rotatedToken:  "the-replacement-raw-token",
		rotatedUserID: "not-a-uuid",
	}
	memberships := newFakeMemberships()

	pair, err := newTestRefresh(store, memberships).Refresh(context.Background(), "the-presented-raw-token")

	assert.Nil(t, pair, "a subject that cannot address a user must not yield a token")
	require.Error(t, err)
	assert.Zero(t, memberships.findCalls)
}
