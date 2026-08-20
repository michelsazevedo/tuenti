package middleware

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const forbiddenErrorCode = "forbidden"

const forbiddenMessage = "You do not have permission to perform this action."

type PermissionCheck func(policy orgdomain.AuthorizationPolicy, role orgdomain.Role) bool

func RequireCanManageOrganization(policy orgdomain.AuthorizationPolicy, role orgdomain.Role) bool {
	return policy.CanManageOrganization(role)
}

func RequireCanManageMembers(policy orgdomain.AuthorizationPolicy, role orgdomain.Role) bool {
	return policy.CanManageMembers(role)
}

func RequireCanManageBilling(policy orgdomain.AuthorizationPolicy, role orgdomain.Role) bool {
	return policy.CanManageBilling(role)
}

func RequireCanViewOrganization(policy orgdomain.AuthorizationPolicy, role orgdomain.Role) bool {
	return policy.CanViewOrganization(role)
}

type forbiddenResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func RequireOrganizationPermission(
	memberships orgdomain.MembershipRepository,
	policy orgdomain.AuthorizationPolicy,
	check PermissionCheck,
) echo.MiddlewareFunc {
	if memberships == nil {
		panic("middleware: RequireOrganizationPermission needs a membership repository")
	}

	if check == nil {
		panic("middleware: RequireOrganizationPermission needs a permission check")
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()

			logger := observability.Logger(ctx)

			identity, err := IdentityFromContext(ctx)
			if err != nil {
				logger.Error().Err(err).
					Str("event", "require_organization_permission_missing_identity").
					Str("path", c.Path()).
					Msg("permission check ran without an authenticated user and organization, RequireAuth must run before it")

				return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
			}

			membership, err := memberships.FindByUserAndOrganization(ctx, identity.CurrentUserID, identity.CurrentOrganizationID)
			if err != nil {
				if errors.Is(err, orgdomain.ErrMembershipNotFound) {
					logger.Warn().
						Str("event", "require_organization_permission_not_a_member").
						Str("path", c.Path()).
						Msg("request blocked, authenticated user has no membership in the target organization")

					return forbidden(c)
				}

				logger.Error().Err(err).
					Str("event", "require_organization_permission_lookup_failed").
					Str("path", c.Path()).
					Msg("could not resolve the membership, failing closed")

				return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
			}

			if !check(policy, membership.Role) {
				logger.Info().
					Str("event", "require_organization_permission_denied").
					Str("path", c.Path()).
					Str("role", string(membership.Role)).
					Msg("request blocked, role does not have the required permission")

				return forbidden(c)
			}

			return next(c)
		}
	}
}

func forbidden(c echo.Context) error {
	return c.JSON(http.StatusForbidden, forbiddenResponse{
		Code:    forbiddenErrorCode,
		Message: forbiddenMessage,
	})
}
