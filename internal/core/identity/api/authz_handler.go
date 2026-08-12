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
}

type authzHandler struct {
	signup  application.SignupUseCase
	signin  application.SigninUseCase
	logout  application.LogoutUseCase
	refresh application.RefreshUseCase
}

func NewAuthzHandler(
	signup application.SignupUseCase,
	signin application.SigninUseCase,
	logout application.LogoutUseCase,
	refresh application.RefreshUseCase,
) AuthzHandler {
	return &authzHandler{signup: signup, signin: signin, logout: logout, refresh: refresh}
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

const msgInvalidRefreshToken = "invalid refresh token"

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
