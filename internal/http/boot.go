package http

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.uber.org/fx"

	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

func NewBoot(lc fx.Lifecycle, cfg *config.Config) *echo.Echo {
	e := echo.New()

	e.Use(middleware.Recover())
	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		RequestIDHandler: func(c echo.Context, rid string) {
			ctx := observability.WithCorrelationID(c.Request().Context(), rid)
			c.SetRequest(c.Request().WithContext(ctx))
		},
	}))
	e.Use(m.Zerolog)
	e.Use(otelecho.Middleware(cfg.Settings.ApplicationName))

	e.HTTPErrorHandler = HTTPErrorHandler

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := e.Start(cfg.Settings.Server.Port); err != nil &&
					err != http.ErrServerClosed {
					e.Logger.Fatal(err)
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			return e.Shutdown(ctx)
		},
	})

	return e
}

func HTTPErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	if httpErr, ok := err.(*echo.HTTPError); ok {
		code := httpErr.Code
		msg := http.StatusText(httpErr.Code)

		c.JSON(code, map[string]string{"message": msg})
	}
}
