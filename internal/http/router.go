package http

import (
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/identity/api"
	orgapi "github.com/michelsazevedo/tuenti/internal/core/organization/api"
)

func RegisterRoutes(e *echo.Echo, healthz HealthzHandler, authz api.AuthzHandler, org orgapi.OrganizationHandler) {
	e.GET("/healthz", healthz.HealthCheck)
	e.POST("/signup", authz.Signup)
	e.POST("/signin", authz.Signin)
	e.POST("/refresh", authz.Refresh)
	e.POST("/logout", authz.Logout)
	e.GET("/organizations/:id", org.GetByID)
}
