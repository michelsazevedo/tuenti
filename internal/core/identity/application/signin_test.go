package application

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

const signinPassword = "correct-horse-battery-staple"

type fakeUserRepository struct {
	user    *domain.User
	findErr error
}

func (f *fakeUserRepository) Create(context.Context, *domain.User) error {
	panic("Create must not be called by the signin use case")
}

func (f *fakeUserRepository) UpdatePasswordDigest(context.Context, pgtype.UUID, string) error {
	panic("UpdatePasswordDigest must not be called by the signin use case")
}

func (f *fakeUserRepository) FindByID(context.Context, pgtype.UUID) (*domain.User, error) {
	panic("FindByID must not be called by the signin use case")
}

func (f *fakeUserRepository) MarkConfirmed(context.Context, pgtype.UUID, time.Time) error {
	panic("MarkConfirmed must not be called by the signin use case")
}

func (f *fakeUserRepository) FindByEmail(context.Context, string) (*domain.User, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}

	return f.user, nil
}

type fakeSaveRefreshTokenStore struct {
	savedToken string
	saveErr    error

	saveCalls    int
	savedSubject string
	saveTTL      time.Duration
}

func (f *fakeSaveRefreshTokenStore) Save(_ context.Context, userID string, ttl time.Duration) (string, error) {
	f.saveCalls++
	f.savedSubject = userID
	f.saveTTL = ttl

	if f.saveErr != nil {
		return "", f.saveErr
	}

	return f.savedToken, nil
}

func (f *fakeSaveRefreshTokenStore) Revoke(context.Context, string) error {
	panic("Revoke must not be called by the signin use case")
}

func (f *fakeSaveRefreshTokenStore) RevokeAllForUser(context.Context, string) error {
	panic("RevokeAllForUser must not be called by the signin use case")
}

func (f *fakeSaveRefreshTokenStore) Validate(context.Context, string) (*domain.RefreshToken, error) {
	panic("Validate must not be called by the signin use case")
}

func (f *fakeSaveRefreshTokenStore) Rotate(context.Context, string, time.Duration) (string, string, error) {
	panic("Rotate must not be called by the signin use case")
}

func newTestUser(t *testing.T) *domain.User {
	t.Helper()

	digest, err := bcrypt.GenerateFromPassword([]byte(signinPassword), bcrypt.MinCost)
	require.NoError(t, err)

	return &domain.User{
		Id:             mustUUID(testUserID),
		Email:          "wile@example.com",
		PasswordDigest: string(digest),
	}
}

func newTestSignin(
	users domain.UserRepository,
	memberships orgdomain.MembershipRepository,
	store domain.RefreshTokenStore,
) SigninUseCase {
	return NewSignin(users, memberships, store, &config.Config{
		Settings: config.Settings{Secret: testSecret, Environment: testEnvironment},
	})
}

func TestSigninSuccess(t *testing.T) {
	users := &fakeUserRepository{user: newTestUser(t)}
	memberships := newFakeMemberships()
	store := &fakeSaveRefreshTokenStore{savedToken: "the-raw-refresh-token"}

	pair, err := newTestSignin(users, memberships, store).SignIn(context.Background(), "wile@example.com", signinPassword)

	require.NoError(t, err)
	require.NotNil(t, pair)

	assert.Equal(t, "the-raw-refresh-token", pair.RefreshToken)
	assert.Equal(t, int64(accessTokenTTL.Seconds()), pair.ExpiresIn)

	claims := parseAccessToken(t, pair.AccessToken)
	assert.Equal(t, testUserID, claims.Subject)
	assert.Equal(t, testOrganizationID, claims.OrganizationID,
		"the access token must carry the organization resolved from the user's membership")

	assert.Equal(t, 1, memberships.findCalls)
	assert.Equal(t, mustUUID(testUserID), memberships.lookedUpUser)

	assert.Equal(t, 1, store.saveCalls)
	assert.Equal(t, testUserID, store.savedSubject)
	assert.Equal(t, refreshTokenTTL, store.saveTTL)
}

func TestSigninRejectsBadCredentials(t *testing.T) {
	tests := []struct {
		name     string
		users    *fakeUserRepository
		password string
	}{
		{
			name:     "unknown email",
			users:    &fakeUserRepository{findErr: domain.ErrUserNotFound},
			password: signinPassword,
		},
		{
			name:     "wrong password",
			users:    &fakeUserRepository{user: newTestUser(t)},
			password: "not-the-password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memberships := newFakeMemberships()
			store := &fakeSaveRefreshTokenStore{}

			pair, err := newTestSignin(tt.users, memberships, store).
				SignIn(context.Background(), "wile@example.com", tt.password)

			assert.Nil(t, pair)
			assert.ErrorIs(t, err, domain.ErrInvalidCredentials,
				"both misses must be indistinguishable to the caller")
			assert.Zero(t, memberships.findCalls, "a failed sign-in must not reach token issuance")
			assert.Zero(t, store.saveCalls)
		})
	}
}

func TestSigninFailsWhenMembershipIsMissing(t *testing.T) {
	users := &fakeUserRepository{user: newTestUser(t)}
	memberships := &fakeMembershipRepository{findErr: orgdomain.ErrMembershipNotFound}
	store := &fakeSaveRefreshTokenStore{}

	pair, err := newTestSignin(users, memberships, store).
		SignIn(context.Background(), "wile@example.com", signinPassword)

	assert.Nil(t, pair, "no organization means no token, rather than a token nothing can authorize")
	assert.ErrorIs(t, err, domain.ErrInvalidCredentials,
		"reporting this differently from a bad password would confirm the password was correct")
	assert.NotErrorIs(t, err, orgdomain.ErrMembershipNotFound,
		"the real reason belongs in the logs, not in the caller's response")
	assert.Zero(t, store.saveCalls, "no refresh token may be minted once issuance has failed")
}

func TestSigninPropagatesMembershipLookupFailures(t *testing.T) {
	users := &fakeUserRepository{user: newTestUser(t)}
	memberships := &fakeMembershipRepository{findErr: errMembershipLookupFailed}
	store := &fakeSaveRefreshTokenStore{}

	pair, err := newTestSignin(users, memberships, store).
		SignIn(context.Background(), "wile@example.com", signinPassword)

	assert.Nil(t, pair)
	assert.ErrorIs(t, err, errMembershipLookupFailed,
		"an infrastructure failure must stay distinguishable from a credential failure")
	assert.NotErrorIs(t, err, domain.ErrInvalidCredentials)
}
