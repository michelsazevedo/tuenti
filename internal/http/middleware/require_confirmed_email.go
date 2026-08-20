package middleware

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const emailNotConfirmedErrorCode = "email_not_confirmed"

const emailNotConfirmedMessage = "Please confirm your email address to continue."

type emailNotConfirmedResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func RequireConfirmedEmail(users identitydomain.UserRepository) echo.MiddlewareFunc {
	if users == nil {
		panic("middleware: RequireConfirmedEmail needs a user repository")
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()

			logger := observability.Logger(ctx)

			identity, err := IdentityFromContext(ctx)
			if err != nil {
				logger.Error().Err(err).
					Str("event", "require_confirmed_email_missing_identity").
					Str("path", c.Path()).
					Msg("require confirmed email ran without an authenticated user, RequireAuth must run before it")

				return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
			}

			user, err := users.FindByID(ctx, identity.CurrentUserID)
			if err != nil {
				event := "require_confirmed_email_lookup_failed"
				if errors.Is(err, identitydomain.ErrUserNotFound) {
					event = "require_confirmed_email_user_not_found"
				}

				logger.Error().Err(err).
					Str("event", event).
					Str("path", c.Path()).
					Msg("could not load the authenticated user, failing closed")

				return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
			}

			if !user.IsConfirmed() {
				logger.Info().
					Str("event", "require_confirmed_email_blocked").
					Str("path", c.Path()).
					Msg("request blocked, user has not confirmed their email address")

				return c.JSON(http.StatusForbidden, emailNotConfirmedResponse{
					Code:    emailNotConfirmedErrorCode,
					Message: emailNotConfirmedMessage,
				})
			}

			return next(c)
		}
	}
}
