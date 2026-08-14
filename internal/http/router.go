package http

import (
	"time"

	"github.com/labstack/echo/v4"
	goredis "github.com/redis/go-redis/v9"

	"github.com/michelsazevedo/tuenti/internal/core/identity/api"
	orgapi "github.com/michelsazevedo/tuenti/internal/core/organization/api"
	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
)

const passwordResetWindow = 15 * time.Minute

func RegisterRoutes(
	e *echo.Echo,
	client *goredis.Client,
	healthz HealthzHandler,
	authz api.AuthzHandler,
	org orgapi.OrganizationHandler,
) {
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

	e.GET("/healthz", healthz.HealthCheck)
	e.POST("/signup", authz.Signup)
	e.POST("/signin", authz.Signin)
	e.POST("/refresh", authz.Refresh)
	e.POST("/logout", authz.Logout)
	e.POST("/password-reset", authz.RequestPasswordReset, requestResetLimit)
	e.POST("/password-reset/confirm", authz.ConfirmPasswordReset, confirmResetLimit)
	e.GET("/organizations/:id", org.GetByID)
}
