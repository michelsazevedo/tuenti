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

type fakeSignupUseCase struct {
	err error

	calls            int
	user             *domain.User
	organizationName string
}

func (f *fakeSignupUseCase) SignUp(_ context.Context, user *domain.User, organizationName string) error {
	f.calls++
	f.user = user
	f.organizationName = organizationName

	if f.err != nil {
		return f.err
	}

	return nil
}

func callSignup(t *testing.T, signup application.SignupUseCase, body string) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := NewAuthzHandler(signup, nil, nil, nil, nil, nil, nil, nil)

	if err := handler.Signup(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func TestSignupForwardsTheOrganizationNameToTheUseCase(t *testing.T) {
	usecase := &fakeSignupUseCase{}

	rec := callSignup(t, usecase, `{
		"name": "Wile E. Coyote",
		"email": "wile@example.com",
		"password": "supersecret",
		"organization_name": "Acme Corp"
	}`)

	require.Equal(t, http.StatusCreated, rec.Code)

	require.Equal(t, 1, usecase.calls)
	assert.Equal(t, "Acme Corp", usecase.organizationName)
	require.NotNil(t, usecase.user)
	assert.Equal(t, "wile@example.com", usecase.user.Email)
	assert.NotContains(t, rec.Body.String(), "Acme Corp", "the signup response shape must stay user-only")
}

func TestSignupRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "unparseable body", body: `{"name":`, code: http.StatusBadRequest},
		{
			name: "missing organization name",
			body: `{"name":"Wile","email":"wile@example.com","password":"supersecret"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "empty organization name",
			body: `{"name":"Wile","email":"wile@example.com","password":"supersecret","organization_name":""}`,
			code: http.StatusUnprocessableEntity,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeSignupUseCase{}

			rec := callSignup(t, usecase, test.body)

			assert.Equal(t, test.code, rec.Code)
			assert.Zero(t, usecase.calls, "a rejected request must never reach the use case")
		})
	}
}

func TestSignupMapsUseCaseFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "duplicate user", err: domain.ErrUserAlreadyExists, code: http.StatusConflict},
		{name: "rolled back transaction", err: errors.New("pgx: forced failure"), code: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeSignupUseCase{err: test.err}

			rec := callSignup(t, usecase, `{
				"name": "Wile E. Coyote",
				"email": "wile@example.com",
				"password": "supersecret",
				"organization_name": "Acme Corp"
			}`)

			assert.Equal(t, test.code, rec.Code)
			assert.NotContains(t, rec.Body.String(), "pgx", "infrastructure detail must not reach the client")
		})
	}
}

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

	handler := NewAuthzHandler(nil, nil, nil, refresh, nil, nil, nil, nil)

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

type fakeRequestPasswordResetUseCase struct {
	err error

	calls int
	email string
}

func (f *fakeRequestPasswordResetUseCase) RequestPasswordReset(_ context.Context, email string) error {
	f.calls++
	f.email = email

	if f.err != nil {
		return f.err
	}

	return nil
}

func callRequestPasswordReset(
	t *testing.T, usecase application.RequestPasswordResetUseCase, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/password-reset", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := NewAuthzHandler(nil, nil, nil, nil, usecase, nil, nil, nil)

	if err := handler.RequestPasswordReset(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func TestRequestPasswordResetIsIndistinguishableAcrossAddresses(t *testing.T) {
	addresses := []struct {
		name  string
		email string
	}{
		{name: "registered address", email: "wile@example.com"},
		{name: "unregistered address", email: "nobody-here@example.com"},
	}

	type response struct {
		code        int
		body        string
		contentType string
	}

	responses := make(map[string]response, len(addresses))

	for _, address := range addresses {
		t.Run(address.name, func(t *testing.T) {
			usecase := &fakeRequestPasswordResetUseCase{}

			rec := callRequestPasswordReset(t, usecase, `{"email":"`+address.email+`"}`)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t,
				`{"message":"if an account exists for this email, a password reset link has been sent"}`,
				rec.Body.String())

			require.Equal(t, 1, usecase.calls)
			assert.Equal(t, address.email, usecase.email, "the address must reach the use case verbatim")
			assert.NotContains(t, rec.Body.String(), address.email,
				"echoing the address back would confirm what the caller submitted was even read")

			responses[address.name] = response{
				code:        rec.Code,
				body:        rec.Body.String(),
				contentType: rec.Header().Get(echo.HeaderContentType),
			}
		})
	}

	require.Len(t, responses, len(addresses))

	baseline := responses["registered address"]

	for name, got := range responses {
		assert.Equal(t, baseline, got,
			"%s must be byte-identical to the registered-address response, or the endpoint enumerates accounts", name)
	}
}

func TestRequestPasswordResetRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "unparseable body", body: `{"email":`, code: http.StatusBadRequest},
		{name: "missing email", body: `{}`, code: http.StatusUnprocessableEntity},
		{name: "empty email", body: `{"email":""}`, code: http.StatusUnprocessableEntity},
		{name: "malformed email", body: `{"email":"not-an-address"}`, code: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeRequestPasswordResetUseCase{}

			rec := callRequestPasswordReset(t, usecase, test.body)

			assert.Equal(t, test.code, rec.Code)
			assert.Zero(t, usecase.calls, "a rejected request must never reach the use case")
		})
	}
}

func TestRequestPasswordResetMapsInfrastructureFailuresTo500(t *testing.T) {
	usecase := &fakeRequestPasswordResetUseCase{err: errors.New("pgx: connection refused")}

	rec := callRequestPasswordReset(t, usecase, `{"email":"wile@example.com"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "pgx", "infrastructure detail must not reach the client")
}

type fakeConfirmPasswordResetUseCase struct {
	err error

	calls       int
	token       string
	newPassword string
}

func (f *fakeConfirmPasswordResetUseCase) ConfirmPasswordReset(_ context.Context, rawToken, newPassword string) error {
	f.calls++
	f.token = rawToken
	f.newPassword = newPassword

	if f.err != nil {
		return f.err
	}

	return nil
}

func callConfirmPasswordReset(
	t *testing.T, usecase application.ConfirmPasswordResetUseCase, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/password-reset/confirm", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := NewAuthzHandler(nil, nil, nil, nil, nil, usecase, nil, nil)

	if err := handler.ConfirmPasswordReset(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func TestConfirmPasswordResetAcceptsAValidToken(t *testing.T) {
	usecase := &fakeConfirmPasswordResetUseCase{}

	rec := callConfirmPasswordReset(t, usecase, `{"token":"presented-token","new_password":"supersecret"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"message":"password has been reset"}`, rec.Body.String())

	require.Equal(t, 1, usecase.calls)
	assert.Equal(t, "presented-token", usecase.token, "the presented token must reach the use case verbatim")
	assert.Equal(t, "supersecret", usecase.newPassword)
	assert.NotContains(t, rec.Body.String(), "supersecret", "the new password must never be echoed back")
}

func TestConfirmPasswordResetRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "unparseable body", body: `{"token":`, code: http.StatusBadRequest},
		{name: "missing token", body: `{"new_password":"supersecret"}`, code: http.StatusUnprocessableEntity},
		{
			name: "empty token",
			body: `{"token":"","new_password":"supersecret"}`,
			code: http.StatusUnprocessableEntity,
		},
		{name: "missing password", body: `{"token":"presented-token"}`, code: http.StatusUnprocessableEntity},
		{
			name: "password under the minimum length",
			body: `{"token":"presented-token","new_password":"short"}`,
			code: http.StatusUnprocessableEntity,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeConfirmPasswordResetUseCase{}

			rec := callConfirmPasswordReset(t, usecase, test.body)

			assert.Equal(t, test.code, rec.Code)
			assert.Zero(t, usecase.calls, "a rejected request must never reach the use case")
		})
	}
}

func TestConfirmPasswordResetTokenFailuresAreIndistinguishable(t *testing.T) {
	failures := []error{
		domain.ErrPasswordResetTokenInvalid,
		domain.ErrPasswordResetTokenExpired,
		domain.ErrPasswordResetTokenUsed,
	}

	type response struct {
		code int
		body string
	}

	responses := make(map[string]response, len(failures))

	for _, failure := range failures {
		t.Run(failure.Error(), func(t *testing.T) {
			usecase := &fakeConfirmPasswordResetUseCase{err: fmt.Errorf("password reset token: %w", failure)}

			rec := callConfirmPasswordReset(t, usecase, `{"token":"presented-token","new_password":"supersecret"}`)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.JSONEq(t, `{"message":"invalid or expired reset token"}`, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), failure.Error(),
				"the specific verdict must not reach the client")

			responses[failure.Error()] = response{code: rec.Code, body: rec.Body.String()}
		})
	}

	require.Len(t, responses, len(failures))

	baseline := responses[domain.ErrPasswordResetTokenInvalid.Error()]
	assert.Equal(t, http.StatusUnauthorized, baseline.code)

	for name, got := range responses {
		assert.Equal(t, baseline, got, "%s must be byte-identical to the plain invalid-token response", name)
	}
}

func TestConfirmPasswordResetMapsInfrastructureFailuresTo500(t *testing.T) {
	usecase := &fakeConfirmPasswordResetUseCase{err: errors.New("pgx: connection refused")}

	rec := callConfirmPasswordReset(t, usecase, `{"token":"presented-token","new_password":"supersecret"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "pgx", "infrastructure detail must not reach the client")
}

type fakeConfirmEmailUseCase struct {
	err error

	calls int
	token string
}

func (f *fakeConfirmEmailUseCase) ConfirmEmail(_ context.Context, rawToken string) error {
	f.calls++
	f.token = rawToken

	if f.err != nil {
		return f.err
	}

	return nil
}

func callConfirmEmail(t *testing.T, usecase application.ConfirmEmailUseCase, query string) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/confirm-email"+query, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := NewAuthzHandler(nil, nil, nil, nil, nil, nil, usecase, nil)

	if err := handler.ConfirmEmail(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func TestConfirmEmailAcceptsAValidToken(t *testing.T) {
	usecase := &fakeConfirmEmailUseCase{}

	rec := callConfirmEmail(t, usecase, "?token=presented-token")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"message":"email confirmed"}`, rec.Body.String())

	require.Equal(t, 1, usecase.calls)
	assert.Equal(t, "presented-token", usecase.token, "the presented token must reach the use case verbatim")
	assert.NotContains(t, rec.Body.String(), "presented-token", "the token must never be echoed back")
}

func TestConfirmEmailRejectsAMissingToken(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "no query string", query: ""},
		{name: "no token parameter", query: "?other=value"},
		{name: "empty token parameter", query: "?token="},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeConfirmEmailUseCase{}

			rec := callConfirmEmail(t, usecase, test.query)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Zero(t, usecase.calls, "a rejected request must never reach the use case")
		})
	}
}

func TestConfirmEmailTokenFailuresAreIndistinguishable(t *testing.T) {
	failures := []error{
		domain.ErrEmailConfirmationTokenInvalid,
		domain.ErrEmailConfirmationTokenExpired,
		domain.ErrEmailConfirmationTokenUsed,
	}

	type response struct {
		code int
		body string
	}

	responses := make(map[string]response, len(failures))

	for _, failure := range failures {
		t.Run(failure.Error(), func(t *testing.T) {
			usecase := &fakeConfirmEmailUseCase{err: fmt.Errorf("email confirmation token: %w", failure)}

			rec := callConfirmEmail(t, usecase, "?token=presented-token")

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.JSONEq(t, `{"message":"invalid or expired confirmation token"}`, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), failure.Error(),
				"the specific verdict must not reach the client")

			responses[failure.Error()] = response{code: rec.Code, body: rec.Body.String()}
		})
	}

	require.Len(t, responses, len(failures))

	baseline := responses[domain.ErrEmailConfirmationTokenInvalid.Error()]
	assert.Equal(t, http.StatusUnauthorized, baseline.code)

	for name, got := range responses {
		assert.Equal(t, baseline, got, "%s must be byte-identical to the plain invalid-token response", name)
	}
}

func TestConfirmEmailMapsInfrastructureFailuresTo500(t *testing.T) {
	usecase := &fakeConfirmEmailUseCase{err: errors.New("pgx: connection refused")}

	rec := callConfirmEmail(t, usecase, "?token=presented-token")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "pgx", "infrastructure detail must not reach the client")
}

type fakeResendConfirmationUseCase struct {
	err error

	calls int
	email string
}

func (f *fakeResendConfirmationUseCase) ResendConfirmation(_ context.Context, email string) error {
	f.calls++
	f.email = email

	if f.err != nil {
		return f.err
	}

	return nil
}

func callResendConfirmation(
	t *testing.T, usecase application.ResendConfirmationUseCase, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/resend-confirmation", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := NewAuthzHandler(nil, nil, nil, nil, nil, nil, nil, usecase)

	if err := handler.ResendConfirmation(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func TestResendConfirmationIsIndistinguishableAcrossAddresses(t *testing.T) {
	addresses := []struct {
		name  string
		email string
	}{
		{name: "registered address", email: "wile@example.com"},
		{name: "unregistered address", email: "nobody-here@example.com"},
	}

	type response struct {
		code        int
		body        string
		contentType string
	}

	responses := make(map[string]response, len(addresses))

	for _, address := range addresses {
		t.Run(address.name, func(t *testing.T) {
			usecase := &fakeResendConfirmationUseCase{}

			rec := callResendConfirmation(t, usecase, `{"email":"`+address.email+`"}`)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t,
				`{"message":"if an account exists for this email, a confirmation link has been sent"}`,
				rec.Body.String())

			require.Equal(t, 1, usecase.calls)
			assert.Equal(t, address.email, usecase.email, "the address must reach the use case verbatim")
			assert.NotContains(t, rec.Body.String(), address.email,
				"echoing the address back would confirm what the caller submitted was even read")

			responses[address.name] = response{
				code:        rec.Code,
				body:        rec.Body.String(),
				contentType: rec.Header().Get(echo.HeaderContentType),
			}
		})
	}

	require.Len(t, responses, len(addresses))

	baseline := responses["registered address"]

	for name, got := range responses {
		assert.Equal(t, baseline, got,
			"%s must be byte-identical to the registered-address response, or the endpoint enumerates accounts", name)
	}
}

func TestResendConfirmationRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "unparseable body", body: `{"email":`, code: http.StatusBadRequest},
		{name: "missing email", body: `{}`, code: http.StatusUnprocessableEntity},
		{name: "empty email", body: `{"email":""}`, code: http.StatusUnprocessableEntity},
		{name: "malformed email", body: `{"email":"not-an-address"}`, code: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeResendConfirmationUseCase{}

			rec := callResendConfirmation(t, usecase, test.body)

			assert.Equal(t, test.code, rec.Code)
			assert.Zero(t, usecase.calls, "a rejected request must never reach the use case")
		})
	}
}

func TestResendConfirmationMapsInfrastructureFailuresTo500(t *testing.T) {
	usecase := &fakeResendConfirmationUseCase{err: errors.New("pgx: connection refused")}

	rec := callResendConfirmation(t, usecase, `{"email":"wile@example.com"}`)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "pgx", "infrastructure detail must not reach the client")
}
