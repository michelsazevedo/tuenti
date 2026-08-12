package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/application"
	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

type fakeRefreshUseCase struct {
	tokens *application.TokenPair
	err    error

	calls     int
	presented string
}

func (f *fakeRefreshUseCase) Refresh(_ context.Context, refreshToken string) (*application.TokenPair, error) {
	f.calls++
	f.presented = refreshToken

	if f.err != nil {
		return nil, f.err
	}

	return f.tokens, nil
}

func callRefresh(t *testing.T, refresh application.RefreshUseCase, body string) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := NewAuthzHandler(nil, nil, nil, refresh)

	if err := handler.Refresh(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func TestRefreshReturnsNewTokenPair(t *testing.T) {
	usecase := &fakeRefreshUseCase{
		tokens: &application.TokenPair{
			AccessToken:  "new-access-token",
			RefreshToken: "rotated-refresh-token",
			ExpiresIn:    900,
		},
	}

	rec := callRefresh(t, usecase, `{"refresh_token":"presented-token"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"access_token": "new-access-token",
		"refresh_token": "rotated-refresh-token",
		"token_type": "Bearer",
		"expires_in": 900
	}`, rec.Body.String())

	assert.Equal(t, 1, usecase.calls)
	assert.Equal(t, "presented-token", usecase.presented, "the presented token must reach the use case verbatim")
}

func TestRefreshRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "unparseable body", body: `{"refresh_token":`, code: http.StatusBadRequest},
		{name: "missing refresh token", body: `{}`, code: http.StatusUnprocessableEntity},
		{name: "empty refresh token", body: `{"refresh_token":""}`, code: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeRefreshUseCase{}

			rec := callRefresh(t, usecase, test.body)

			assert.Equal(t, test.code, rec.Code)
			assert.Zero(t, usecase.calls, "a rejected request must never reach the use case")
		})
	}
}

func TestRefreshTokenFailuresAreIndistinguishable(t *testing.T) {
	failures := []error{
		domain.ErrRefreshTokenInvalid,
		domain.ErrRefreshTokenExpired,
		domain.ErrRefreshTokenRevoked,
		domain.ErrRefreshTokenReused,
	}

	type response struct {
		code int
		body string
	}

	responses := make(map[string]response, len(failures))

	for _, failure := range failures {
		t.Run(failure.Error(), func(t *testing.T) {
			usecase := &fakeRefreshUseCase{err: fmt.Errorf("refresh token store: %w", failure)}

			rec := callRefresh(t, usecase, `{"refresh_token":"presented-token"}`)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.JSONEq(t, `{"message":"invalid refresh token"}`, rec.Body.String())

			responses[failure.Error()] = response{code: rec.Code, body: rec.Body.String()}
		})
	}

	require.Len(t, responses, len(failures))

	baseline := responses[domain.ErrRefreshTokenInvalid.Error()]
	assert.Equal(t, http.StatusUnauthorized, baseline.code)

	for name, got := range responses {
		assert.Equal(t, baseline, got, "%s must be byte-identical to the plain invalid-token response", name)
	}
}

func TestRefreshMapsInfrastructureFailuresTo500(t *testing.T) {
	usecase := &fakeRefreshUseCase{err: errors.New("redis: connection refused")}

	rec := callRefresh(t, usecase, `{"refresh_token":"presented-token"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "redis", "infrastructure detail must not reach the client")
}
