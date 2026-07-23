package http

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type HealthzHandler interface {
	HealthCheck(c echo.Context) error
}

type healthzHandler struct{}

func NewHealthzHandler() HealthzHandler {
	return &healthzHandler{}
}

func (h *healthzHandler) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
