package http

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog/log"
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

	code := http.StatusInternalServerError

	if httpErr, ok := err.(*echo.HTTPError); ok {
		code = httpErr.Code
	} else {
		event := log.Error().Err(err).
			Str("method", c.Request().Method).
			Str("uri", c.Request().RequestURI)

		if id := observability.CorrelationID(c.Request().Context()); id != "" {
			event = event.Str("correlation_id", id)
		}

		event.Msg("unhandled error, responding 500")
	}

	if err := c.JSON(code, map[string]string{"message": http.StatusText(code)}); err != nil {
		log.Error().Err(err).Msg("failed to write error response")
	}
}
