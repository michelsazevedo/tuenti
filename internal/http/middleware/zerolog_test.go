package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestURIRedactsSensitiveQueryParams(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "no query", target: "/customers", want: "/customers"},
		{name: "harmless query is kept verbatim", target: "/customers?page=2", want: "/customers?page=2"},
		{name: "token", target: "/auth/confirm?token=abc123", want: "/auth/confirm?token=%5BREDACTED%5D"},
		{name: "secret", target: "/x?secret=hunter2", want: "/x?secret=%5BREDACTED%5D"},
		{name: "access_token", target: "/x?access_token=eyJ", want: "/x?access_token=%5BREDACTED%5D"},
		{name: "code", target: "/x?code=4242", want: "/x?code=%5BREDACTED%5D"},
		{
			name:   "search carries customer PII",
			target: "/customers?search=jane.doe%40acme.com",
			want:   "/customers?search=%5BREDACTED%5D",
		},
		{
			name:   "search is redacted alongside params that are kept",
			target: "/customers?page=2&search=jane",
			want:   "/customers?page=2&search=%5BREDACTED%5D",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequestURI(httptest.NewRequest(http.MethodGet, tt.target, nil))

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestZerologAccessLogDoesNotLeakTheSearchTerm swaps the global logger, so it
// must not run in parallel.
func TestZerologAccessLogDoesNotLeakTheSearchTerm(t *testing.T) {
	var buf bytes.Buffer

	previous := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = previous })

	e := echo.New()
	handler := Zerolog(func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/customers?search=jane.doe%40acme.com", nil)
	require.NoError(t, handler(e.NewContext(request, httptest.NewRecorder())))

	assert.NotContains(t, buf.String(), "jane.doe", "the access log must never carry a raw search term")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry), buf.String())
	assert.Equal(t, "/customers?search=%5BREDACTED%5D", entry["uri"])
}
