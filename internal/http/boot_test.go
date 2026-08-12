package http

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(handler echo.HandlerFunc) *echo.Echo {
	e := echo.New()
	e.Use(middleware.Recover())
	e.HTTPErrorHandler = HTTPErrorHandler
	e.POST("/boom", handler)

	return e
}

func call(t *testing.T, e *echo.Echo) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/boom", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

func TestHTTPErrorHandlerFailsClosedOnNonHTTPError(t *testing.T) {
	tests := []struct {
		name    string
		handler echo.HandlerFunc
	}{
		{
			name:    "plain error from a handler",
			handler: func(echo.Context) error { return errors.New("boom") },
		},
		{
			name:    "wrapped sentinel error",
			handler: func(echo.Context) error { return errors.New("redis: connection refused") },
		},
		{
			name:    "panic recovered by middleware.Recover",
			handler: func(echo.Context) error { panic("boom") },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := call(t, newTestServer(test.handler))

			require.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.JSONEq(t, `{"message":"Internal Server Error"}`, rec.Body.String())
			assert.Equal(t, echo.MIMEApplicationJSON, rec.Header().Get(echo.HeaderContentType))
		})
	}
}

func TestHTTPErrorHandlerDoesNotLeakInternalDetail(t *testing.T) {
	handler := func(echo.Context) error {
		return errors.New("dial tcp 10.0.0.7:5432: connection refused")
	}

	rec := call(t, newTestServer(handler))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "10.0.0.7")
	assert.NotContains(t, body, "connection refused")
}

func TestHTTPErrorHandlerLogsTheUnderlyingError(t *testing.T) {
	var buf bytes.Buffer

	previous := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = previous })

	handler := func(echo.Context) error { return errors.New("dial tcp: connection refused") }

	rec := call(t, newTestServer(handler))

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	logged := buf.String()
	assert.Contains(t, logged, "dial tcp: connection refused", "the cause must reach the logs")
	assert.Contains(t, logged, `"level":"error"`, "an unhandled failure is an ERROR, not an INFO")
}

func TestHTTPErrorHandlerPreservesHTTPErrorStatus(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{name: "unauthorized", code: http.StatusUnauthorized},
		{name: "not found", code: http.StatusNotFound},
		{name: "unprocessable entity", code: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := func(echo.Context) error { return echo.NewHTTPError(test.code) }

			rec := call(t, newTestServer(handler))

			require.Equal(t, test.code, rec.Code)
			assert.JSONEq(t, `{"message":"`+http.StatusText(test.code)+`"}`, rec.Body.String())
		})
	}
}

func TestHTTPErrorHandlerLeavesCommittedResponsesAlone(t *testing.T) {
	handler := func(c echo.Context) error {
		if err := c.JSON(http.StatusOK, map[string]string{"status": "ok"}); err != nil {
			return err
		}

		return errors.New("failed after responding")
	}

	rec := call(t, newTestServer(handler))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}
