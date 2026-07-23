package http

import "github.com/labstack/echo/v4"

func RegisterRoutes(e *echo.Echo, healthz HealthzHandler) {
	e.GET("/healthz", healthz.HealthCheck)
}
