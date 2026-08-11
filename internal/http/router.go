package http

import (
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/identity/api"
)

func RegisterRoutes(e *echo.Echo, healthz HealthzHandler, authz api.AuthzHandler) {
	e.GET("/healthz", healthz.HealthCheck)
	e.POST("/signup", authz.Signup)
	e.POST("/signin", authz.Signin)
}
