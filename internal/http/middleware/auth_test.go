package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/application"
)

const (
	authSecret = "s3cr3t-signing-key"

	authUserID = "6f1c9a2e-1f4b-4d3a-9c11-2b8a7d5e0f33"

	authOrganizationID = "0b7d3f18-5a2c-4c6e-8f90-1d4e6a9c2b55"

	authEnvironment = "test"
)

type authenticatedCall struct {
	status   bool
	code     int
	identity Identity
	identErr error
}

func signToken(t *testing.T, method jwt.SigningMethod, key any, claims jwt.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	require.NoError(t, err)

	return token
}

func validClaims(expiresAt time.Time) *application.AccessTokenClaims {
	return &application.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    application.TokenIssuer(authEnvironment),
			Audience:  jwt.ClaimStrings{application.TokenAudience},
			Subject:   authUserID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
		OrganizationID: authOrganizationID,
	}
}

func validToken(t *testing.T) string {
	t.Helper()

	return signToken(t, jwt.SigningMethodHS256, []byte(authSecret), validClaims(time.Now().Add(15*time.Minute)))
}

func serveWithAuth(t *testing.T, authorization string) authenticatedCall {
	t.Helper()

	e := echo.New()

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authorization != "" {
		request.Header.Set(echo.HeaderAuthorization, authorization)
	}

	rec := httptest.NewRecorder()
	c := e.NewContext(request, rec)

	result := authenticatedCall{}

	handler := RequireAuth(authSecret, authEnvironment)(func(c echo.Context) error {
		result.status = true

		return c.NoContent(http.StatusOK)
	})

	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	result.code = rec.Code
	result.identity, result.identErr = IdentityFromContext(c.Request().Context())

	return result
}

func mustUUID(t *testing.T, raw string) pgtype.UUID {
	t.Helper()

	var id pgtype.UUID
	require.NoError(t, id.Scan(raw))

	return id
}

func TestRequireAuthAcceptsValidToken(t *testing.T) {
	call := serveWithAuth(t, "Bearer "+validToken(t))

	require.True(t, call.status, "a valid token must reach the handler")
	assert.Equal(t, http.StatusOK, call.code)
	require.NoError(t, call.identErr)
	assert.Equal(t, mustUUID(t, authUserID), call.identity.CurrentUserID, "the subject must be published as the current user")
	assert.Equal(t, mustUUID(t, authOrganizationID), call.identity.CurrentOrganizationID,
		"the token's organization must be published")
}

func TestRequireAuthAcceptsLowercaseScheme(t *testing.T) {
	call := serveWithAuth(t, "bearer "+validToken(t))

	assert.True(t, call.status)
	assert.Equal(t, http.StatusOK, call.code)
}

func TestRequireAuthRejects(t *testing.T) {
	tests := []struct {
		name          string
		authorization func(t *testing.T) string
	}{
		{
			name:          "no authorization header",
			authorization: func(*testing.T) string { return "" },
		},
		{
			name:          "header without a token",
			authorization: func(*testing.T) string { return "Bearer" },
		},
		{
			name:          "empty bearer credentials",
			authorization: func(*testing.T) string { return "Bearer   " },
		},
		{
			name: "wrong scheme",
			authorization: func(t *testing.T) string {
				return "Basic " + validToken(t)
			},
		},
		{
			name:          "malformed token",
			authorization: func(*testing.T) string { return "Bearer not-even-a-jwt" },
		},
		{
			name: "expired token",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(-time.Minute))

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "token without an expiry never expires",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.ExpiresAt = nil

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "signed with another secret",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte("some-other-key"), claims)
			},
		},
		{
			name: "unsigned alg none token",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))

				return "Bearer " + signToken(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims)
			},
		},
		{
			name: "another hmac algorithm",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))

				return "Bearer " + signToken(t, jwt.SigningMethodHS512, []byte(authSecret), claims)
			},
		},
		{
			name: "not valid yet",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Minute))

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "valid signature but no organization claim",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.OrganizationID = ""

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "signed correctly but issued by another environment",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.Issuer = application.TokenIssuer("development")

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "no issuer at all",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.Issuer = ""

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "signed correctly but for another audience",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.Audience = jwt.ClaimStrings{"some-other-service"}

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "no audience at all",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.Audience = nil

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
		{
			name: "valid signature but no subject",
			authorization: func(t *testing.T) string {
				claims := validClaims(time.Now().Add(time.Hour))
				claims.Subject = ""

				return "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte(authSecret), claims)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := serveWithAuth(t, tt.authorization(t))

			assert.False(t, call.status, "the handler must not run for an unauthenticated request")
			assert.Equal(t, http.StatusUnauthorized, call.code)
			assert.ErrorIs(t, call.identErr, ErrIdentityMissing)
		})
	}
}

func TestRequireAuthIgnoresAPreSeededIdentity(t *testing.T) {
	e := echo.New()

	spoofed := Identity{
		CurrentUserID:         mustUUID(t, authUserID),
		CurrentOrganizationID: mustUUID(t, authOrganizationID),
	}

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request = request.WithContext(WithIdentity(request.Context(), spoofed))

	c := e.NewContext(request, httptest.NewRecorder())

	handler := RequireAuth(authSecret, authEnvironment)(func(echo.Context) error {
		t.Fatal("the handler must not run without a token")

		return nil
	})

	require.Error(t, handler(c), "a pre-seeded identity must not let the request skip token validation")
}

func TestRequireAuthPanicsOnBrokenWiring(t *testing.T) {
	tests := []struct {
		name        string
		secret      string
		environment string
	}{
		{name: "no signing secret", environment: authEnvironment},
		{name: "no environment", secret: authSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() { RequireAuth(tt.secret, tt.environment) })
		})
	}
}
