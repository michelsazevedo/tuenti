package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/identity/application"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const bearerScheme = "Bearer"

type Identity struct {
	CurrentUserID         pgtype.UUID
	CurrentOrganizationID pgtype.UUID
}

type identityContextKey struct{}

var ErrIdentityMissing = errors.New("no authenticated identity on the request context")

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (Identity, error) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	if !ok {
		return Identity{}, ErrIdentityMissing
	}

	return identity, nil
}

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

			var identity Identity

			if err := identity.CurrentUserID.Scan(claims.Subject); err != nil {
				logger.Warn().Err(err).
					Str("event", "auth_claims_incomplete").
					Str("remote_ip", c.RealIP()).
					Str("path", c.Path()).
					Msg("request rejected with a token missing or carrying an unparsable subject")

				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
			}

			if err := identity.CurrentOrganizationID.Scan(claims.OrganizationID); err != nil {
				logger.Warn().Err(err).
					Str("event", "auth_claims_incomplete").
					Str("remote_ip", c.RealIP()).
					Str("path", c.Path()).
					Msg("request rejected with a token missing or carrying an unparsable organization")

				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
			}

			c.SetRequest(c.Request().WithContext(WithIdentity(c.Request().Context(), identity)))

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
