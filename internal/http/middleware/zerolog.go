package middleware

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"

	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

var redactedQueryParams = []string{"token", "code", "secret", "access_token"}

func RequestURI(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return r.URL.Path
	}

	query := r.URL.Query()

	redacted := false

	for _, key := range redactedQueryParams {
		if query.Has(key) {
			query.Set(key, "[REDACTED]")
			redacted = true
		}
	}

	if !redacted {
		return r.URL.Path + "?" + r.URL.RawQuery
	}

	return r.URL.Path + "?" + query.Encode()
}

func Zerolog(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		start := time.Now()

		err := next(c)
		stop := time.Now()

		status := c.Response().Status
		level := log.Info()

		if err != nil {
			if httpErr, ok := err.(*echo.HTTPError); ok {
				status = httpErr.Code
			}
		}

		switch {
		case status >= 500:
			level = log.Error()
		case status >= 400:
			level = log.Warn()
		}

		event := level.
			Err(err).
			Str("method", c.Request().Method).
			Str("uri", RequestURI(c.Request())).
			Str("remote_ip", c.RealIP()).
			Str("user_agent", c.Request().UserAgent()).
			Int("status", status).
			Dur("latency", stop.Sub(start)).
			Str("host", c.Request().Host)

		if sc := trace.SpanContextFromContext(c.Request().Context()); sc.IsValid() {
			event = event.
				Str("trace_id", sc.TraceID().String()).
				Str("span_id", sc.SpanID().String())
		}

		if id := observability.CorrelationID(c.Request().Context()); id != "" {
			event = event.Str("correlation_id", id)
		}

		event.Msg("")

		return err
	}
}
