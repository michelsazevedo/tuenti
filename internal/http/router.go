package http

import (
	"time"

	"github.com/labstack/echo/v4"
	goredis "github.com/redis/go-redis/v9"

	"github.com/michelsazevedo/tuenti/internal/core/identity/api"
	orgapi "github.com/michelsazevedo/tuenti/internal/core/organization/api"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

const passwordResetWindow = 15 * time.Minute

func RegisterRoutes(
	e *echo.Echo,
	client *goredis.Client,
	cfg *config.Config,
	memberships orgdomain.MembershipRepository,
	healthz HealthzHandler,
	authz api.AuthzHandler,
	org orgapi.OrganizationHandler,
	inv orgapi.InvitationHandler,
) {
	requireAuth := m.RequireAuth(cfg.Settings.Secret, cfg.Settings.Environment)
	requireManagerOrAdmin := m.RequireOrganizationPermission(memberships, orgdomain.AuthorizationPolicy{}, m.RequireCanManageMembers)

	requestResetLimit := m.RateLimit(client, m.RateLimitConfig{
		Max:       5,
		Window:    passwordResetWindow,
		KeyPrefix: "ratelimit:password-reset",
		Extractor: m.EmailAndIPKeyExtractor,
	})
	confirmResetLimit := m.RateLimit(client, m.RateLimitConfig{
		Max:       10,
		Window:    passwordResetWindow,
		KeyPrefix: "ratelimit:password-reset-confirm",
		Extractor: m.IPKeyExtractor,
	})
	resendConfirmationLimit := m.RateLimit(client, m.RateLimitConfig{
		Max:       5,
		Window:    passwordResetWindow,
		KeyPrefix: "ratelimit:resend-confirmation",
		Extractor: m.EmailAndIPKeyExtractor,
	})

	createInvitationLimit := m.RateLimit(client, m.RateLimitConfig{
		Max:       20,
		Window:    time.Hour,
		KeyPrefix: "ratelimit:create-invitation",
		Extractor: m.OrganizationKeyExtractor,
	})
	acceptInvitationLimit := m.RateLimit(client, m.RateLimitConfig{
		Max:       10,
		Window:    passwordResetWindow,
		KeyPrefix: "ratelimit:accept-invitation",
		Extractor: m.IPKeyExtractor,
	})

	e.GET("/healthz", healthz.HealthCheck)

	auth := e.Group("/auth")
	auth.POST("/signup", authz.Signup)
	auth.POST("/signin", authz.Signin)
	auth.POST("/refresh", authz.Refresh)
	auth.POST("/logout", authz.Logout)
	auth.POST("/password-reset", authz.RequestPasswordReset, requestResetLimit)
	auth.POST("/password-reset/confirm", authz.ConfirmPasswordReset, confirmResetLimit)
	auth.GET("/confirm-email", authz.ConfirmEmail)
	auth.POST("/resend-confirmation", authz.ResendConfirmation, resendConfirmationLimit)

	organizations := e.Group("/organizations")
	organizations.GET("/:id", org.GetByID)
	organizations.POST("/:organizationId/invitations", inv.Create, requireAuth, requireManagerOrAdmin, createInvitationLimit)
	organizations.GET("/:organizationId/invitations", inv.List, requireAuth, requireManagerOrAdmin)
	organizations.DELETE("/:organizationId/invitations/:id", inv.Revoke, requireAuth, requireManagerOrAdmin)

	e.POST("/invitations/accept", inv.Accept, acceptInvitationLimit)
}
