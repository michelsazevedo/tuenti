package api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/identity/application"
	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

type AuthzHandler interface {
	Signup(c echo.Context) error
	Signin(c echo.Context) error
	Logout(c echo.Context) error
	Refresh(c echo.Context) error
	RequestPasswordReset(c echo.Context) error
	ConfirmPasswordReset(c echo.Context) error
	ConfirmEmail(c echo.Context) error
	ResendConfirmation(c echo.Context) error
}

type authzHandler struct {
	signup               application.SignupUseCase
	signin               application.SigninUseCase
	logout               application.LogoutUseCase
	refresh              application.RefreshUseCase
	requestPasswordReset application.RequestPasswordResetUseCase
	confirmPasswordReset application.ConfirmPasswordResetUseCase
	confirmEmail         application.ConfirmEmailUseCase
	resendConfirmation   application.ResendConfirmationUseCase
}

func NewAuthzHandler(
	signup application.SignupUseCase,
	signin application.SigninUseCase,
	logout application.LogoutUseCase,
	refresh application.RefreshUseCase,
	requestPasswordReset application.RequestPasswordResetUseCase,
	confirmPasswordReset application.ConfirmPasswordResetUseCase,
	confirmEmail application.ConfirmEmailUseCase,
	resendConfirmation application.ResendConfirmationUseCase,
) AuthzHandler {
	return &authzHandler{
		signup:               signup,
		signin:               signin,
		logout:               logout,
		refresh:              refresh,
		requestPasswordReset: requestPasswordReset,
		confirmPasswordReset: confirmPasswordReset,
		confirmEmail:         confirmEmail,
		resendConfirmation:   resendConfirmation,
	}
}

func (h *authzHandler) Signup(c echo.Context) error {
	req := new(SignupRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	user := req.ToDomain()

	if err := h.signup.SignUp(c.Request().Context(), user, req.OrganizationName); err != nil {
		if errors.Is(err, domain.ErrUserAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "user already exists")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusCreated, NewUserResponse(user))
}

func (h *authzHandler) Signin(c echo.Context) error {
	req := new(SigninRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	tokens, err := h.signin.SignIn(c.Request().Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, NewTokenResponse(tokens))
}

func (h *authzHandler) Logout(c echo.Context) error {
	req := new(LogoutRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	if err := h.logout.Logout(c.Request().Context(), req.RefreshToken); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *authzHandler) Refresh(c echo.Context) error {
	req := new(RefreshRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	tokens, err := h.refresh.Refresh(c.Request().Context(), req.RefreshToken)
	if err != nil {
		if isRefreshTokenFailure(err) {
			return echo.NewHTTPError(http.StatusUnauthorized, msgInvalidRefreshToken)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, NewTokenResponse(tokens))
}

func (h *authzHandler) RequestPasswordReset(c echo.Context) error {
	req := new(RequestPasswordResetRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	if err := h.requestPasswordReset.RequestPasswordReset(c.Request().Context(), req.Email); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, MessageResponse{Message: msgPasswordResetRequested})
}

func (h *authzHandler) ConfirmPasswordReset(c echo.Context) error {
	req := new(ConfirmPasswordResetRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	if err := h.confirmPasswordReset.ConfirmPasswordReset(c.Request().Context(), req.Token, req.NewPassword); err != nil {
		if isPasswordResetTokenFailure(err) {
			return echo.NewHTTPError(http.StatusUnauthorized, msgInvalidPasswordResetToken)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, MessageResponse{Message: msgPasswordResetCompleted})
}

func (h *authzHandler) ConfirmEmail(c echo.Context) error {
	token := c.QueryParam("token")

	if token == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing token")
	}

	if err := h.confirmEmail.ConfirmEmail(c.Request().Context(), token); err != nil {
		if isEmailConfirmationTokenFailure(err) {
			return echo.NewHTTPError(http.StatusUnauthorized, msgInvalidEmailConfirmationToken)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, MessageResponse{Message: msgEmailConfirmed})
}

func (h *authzHandler) ResendConfirmation(c echo.Context) error {
	req := new(ResendConfirmationRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	if err := h.resendConfirmation.ResendConfirmation(c.Request().Context(), req.Email); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, MessageResponse{Message: msgConfirmationResent})
}

const msgInvalidRefreshToken = "invalid refresh token"

const (
	msgPasswordResetRequested = "if an account exists for this email, a password reset link has been sent"
	msgPasswordResetCompleted = "password has been reset"

	msgInvalidPasswordResetToken = "invalid or expired reset token"
)

const (
	msgEmailConfirmed     = "email confirmed"
	msgConfirmationResent = "if an account exists for this email, a confirmation link has been sent"

	msgInvalidEmailConfirmationToken = "invalid or expired confirmation token"
)

var refreshTokenFailures = []error{
	domain.ErrRefreshTokenInvalid,
	domain.ErrRefreshTokenExpired,
	domain.ErrRefreshTokenRevoked,
	domain.ErrRefreshTokenReused,
}

func isRefreshTokenFailure(err error) bool {
	for _, refreshErr := range refreshTokenFailures {
		if errors.Is(err, refreshErr) {
			return true
		}
	}

	return false
}

var passwordResetTokenFailures = []error{
	domain.ErrPasswordResetTokenInvalid,
	domain.ErrPasswordResetTokenExpired,
	domain.ErrPasswordResetTokenUsed,
}

func isPasswordResetTokenFailure(err error) bool {
	for _, tokenErr := range passwordResetTokenFailures {
		if errors.Is(err, tokenErr) {
			return true
		}
	}

	return false
}

var emailConfirmationTokenFailures = []error{
	domain.ErrEmailConfirmationTokenInvalid,
	domain.ErrEmailConfirmationTokenExpired,
	domain.ErrEmailConfirmationTokenUsed,
}

func isEmailConfirmationTokenFailure(err error) bool {
	for _, tokenErr := range emailConfirmationTokenFailures {
		if errors.Is(err, tokenErr) {
			return true
		}
	}

	return false
}
