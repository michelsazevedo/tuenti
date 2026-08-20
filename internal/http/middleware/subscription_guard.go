package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const subscriptionRequiredErrorCode = "subscription_required"

type subscriptionRequiredResponse struct {
	Error              string                       `json:"error"`
	SubscriptionStatus orgdomain.SubscriptionStatus `json:"subscription_status"`
}

func SubscriptionGuard(orgs orgdomain.OrganizationRepository, policy orgdomain.OrganizationAccessPolicy) echo.MiddlewareFunc {
	if orgs == nil {
		panic("middleware: SubscriptionGuard needs an organization repository")
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()

			logger := observability.Logger(ctx)

			identity, err := IdentityFromContext(ctx)
			if err != nil {
				logger.Error().Err(err).
					Str("event", "subscription_guard_missing_identity").
					Str("path", c.Path()).
					Msg("subscription guard ran without an authenticated organization, RequireAuth must run before it")

				return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
			}

			organization, err := orgs.FindByID(ctx, identity.CurrentOrganizationID)
			if err != nil {
				if errors.Is(err, orgdomain.ErrOrganizationNotFound) {
					logger.Warn().
						Str("event", "subscription_guard_organization_not_found").
						Str("path", c.Path()).
						Msg("token names an organization that does not exist")

					return echo.NewHTTPError(http.StatusNotFound, "organization not found")
				}

				logger.Error().Err(err).
					Str("event", "subscription_guard_lookup_failed").
					Str("path", c.Path()).
					Msg("could not load the organization, failing closed")

				return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
			}

			if !policy.CanPerformBusinessOperations(organization, time.Now().UTC()) {
				logger.Info().
					Str("event", "subscription_guard_blocked").
					Str("path", c.Path()).
					Str("subscription_status", string(organization.SubscriptionStatus)).
					Msg("request blocked, organization may not perform business operations")

				return c.JSON(http.StatusPaymentRequired, subscriptionRequiredResponse{
					Error:              subscriptionRequiredErrorCode,
					SubscriptionStatus: organization.SubscriptionStatus,
				})
			}

			return next(c)
		}
	}
}
