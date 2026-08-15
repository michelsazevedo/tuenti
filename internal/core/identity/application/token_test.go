package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const (
	testSecret = "s3cr3t-signing-key"

	testUserID = "6f1c9a2e-1f4b-4d3a-9c11-2b8a7d5e0f33"

	testOrganizationID = "0b7d3f18-5a2c-4c6e-8f90-1d4e6a9c2b55"

	testEnvironment = "test"
)

var errMembershipLookupFailed = errors.New("postgres: connection refused")

type fakeMembershipRepository struct {
	organizationID string
	findErr        error

	findCalls    int
	lookedUpUser pgtype.UUID
}

func (f *fakeMembershipRepository) Create(context.Context, *orgdomain.Membership) error {
	panic("Create must not be called while issuing a token")
}

func (f *fakeMembershipRepository) FindByUserID(_ context.Context, userID pgtype.UUID) (*orgdomain.Membership, error) {
	f.findCalls++
	f.lookedUpUser = userID

	if f.findErr != nil {
		return nil, f.findErr
	}

	return &orgdomain.Membership{
		OrganizationId: mustUUID(f.organizationID),
		UserId:         userID,
		Role:           orgdomain.RoleManager,
	}, nil
}

func (f *fakeMembershipRepository) FindByUserAndOrganization(
	context.Context,
	pgtype.UUID,
	pgtype.UUID,
) (*orgdomain.Membership, error) {
	panic("FindByUserAndOrganization must not be called while issuing a token")
}

func newFakeMemberships() *fakeMembershipRepository {
	return &fakeMembershipRepository{organizationID: testOrganizationID}
}

func mustUUID(value string) pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		panic(err)
	}

	return id
}

func parseAccessToken(t *testing.T, accessToken string) *AccessTokenClaims {
	t.Helper()

	claims := &AccessTokenClaims{}

	parsed, err := jwt.ParseWithClaims(accessToken, claims, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(TokenIssuer(testEnvironment)),
		jwt.WithAudience(TokenAudience),
	)

	require.NoError(t, err)
	require.True(t, parsed.Valid)

	return claims
}

func TestNewAccessTokenCarriesOrganization(t *testing.T) {
	token, err := newAccessToken(testSecret, testUserID, testOrganizationID, testEnvironment)

	require.NoError(t, err)

	claims := parseAccessToken(t, token)

	assert.Equal(t, testUserID, claims.Subject)
	assert.Equal(t, testOrganizationID, claims.OrganizationID)
	assert.WithinDuration(t, time.Now().Add(accessTokenTTL), claims.ExpiresAt.Time, time.Minute)
	require.NotNil(t, claims.IssuedAt)
	assert.True(t, claims.ExpiresAt.After(claims.IssuedAt.Time))
}

func TestNewAccessTokenBindsIssuerAndAudience(t *testing.T) {
	token, err := newAccessToken(testSecret, testUserID, testOrganizationID, testEnvironment)

	require.NoError(t, err)

	claims := parseAccessToken(t, token)

	assert.Equal(t, "tuenti/test", claims.Issuer, "the token must name the environment that minted it")
	assert.Equal(t, jwt.ClaimStrings{TokenAudience}, claims.Audience)
}

func TestNewAccessTokenIssuerIsEnvironmentSpecific(t *testing.T) {
	issuers := make(map[string]string, 3)

	for _, environment := range []string{"development", "test", "production"} {
		token, err := newAccessToken(testSecret, testUserID, testOrganizationID, environment)
		require.NoError(t, err)

		claims := &AccessTokenClaims{}

		_, err = jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
			return []byte(testSecret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		require.NoError(t, err)

		previous, seen := issuers[claims.Issuer]
		require.False(t, seen, "%q and %q must not share the issuer %q", previous, environment, claims.Issuer)

		issuers[claims.Issuer] = environment
	}
}

func TestNewAccessTokenRejectsIncompleteInput(t *testing.T) {
	tests := []struct {
		name           string
		secret         string
		subject        string
		organizationID string
		wantErr        error
	}{
		{
			name:           "no signing secret",
			subject:        testUserID,
			organizationID: testOrganizationID,
			wantErr:        ErrMissingSigningSecret,
		},
		{
			name:           "no subject",
			secret:         testSecret,
			organizationID: testOrganizationID,
			wantErr:        ErrIncompleteTokenClaims,
		},
		{
			name:    "no organization id",
			secret:  testSecret,
			subject: testUserID,
			wantErr: ErrIncompleteTokenClaims,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := newAccessToken(tt.secret, tt.subject, tt.organizationID, testEnvironment)

			assert.Empty(t, token, "no token may be minted from incomplete claims")
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestIssueAccessTokenPropagatesMembershipFailures(t *testing.T) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{name: "user without a membership", wantErr: orgdomain.ErrMembershipNotFound},
		{name: "infrastructure failure", wantErr: errMembershipLookupFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memberships := &fakeMembershipRepository{findErr: tt.wantErr}

			token, err := issueAccessToken(
				context.Background(), memberships, testSecret, testEnvironment, mustUUID(testUserID),
			)

			assert.Empty(t, token, "a token must never be minted without a resolved organization")
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
