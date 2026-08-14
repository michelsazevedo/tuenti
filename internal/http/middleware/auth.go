package middleware

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/identity/application"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const (
	ContextKeyUserID = "user_id"

	ContextKeyOrganizationID = "organization_id"

	bearerScheme = "Bearer"
)

func RequireAuth(secret, environment string) echo.MiddlewareFunc {
	if secret == "" {
		panic("middleware: RequireAuth needs a signing secret")
	}

	if environment == "" {
		panic("middleware: RequireAuth needs an environment to pin the issuer to")
	}

	key := []byte(secret)

	keyFunc := func(*jwt.Token) (any, error) { return key, nil }

	options := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(application.TokenIssuer(environment)),
		jwt.WithAudience(application.TokenAudience),
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(ContextKeyUserID, nil)
			c.Set(ContextKeyOrganizationID, nil)

			logger := observability.Logger(c.Request().Context())

			rawToken, ok := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
			if !ok {
				logger.Debug().
					Str("event", "auth_missing_bearer_token").
					Str("remote_ip", c.RealIP()).
					Str("path", c.Path()).
					Msg("request rejected without a bearer token")

				return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
			}

			claims := &application.AccessTokenClaims{}

			token, err := jwt.ParseWithClaims(rawToken, claims, keyFunc, options...)
			if err != nil || !token.Valid {
				logger.Warn().Err(err).
					Str("event", "auth_token_rejected").
					Str("remote_ip", c.RealIP()).
					Str("path", c.Path()).
					Msg("request rejected with an unverifiable token")

				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
			}

			if claims.Subject == "" || claims.OrganizationID == "" {
				logger.Warn().
					Str("event", "auth_claims_incomplete").
					Str("remote_ip", c.RealIP()).
					Str("path", c.Path()).
					Msg("request rejected with a token missing subject or organization")

				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
			}

			c.Set(ContextKeyUserID, claims.Subject)
			c.Set(ContextKeyOrganizationID, claims.OrganizationID)

			return next(c)
		}
	}
}

func bearerToken(header string) (string, bool) {
	scheme, credentials, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) {
		return "", false
	}

	credentials = strings.TrimSpace(credentials)
	if credentials == "" {
		return "", false
	}

	return credentials, true
}
