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
}

type authzHandler struct {
	signup application.SignupUseCase
	signin application.SigninUseCase
}

func NewAuthzHandler(signup application.SignupUseCase, signin application.SigninUseCase) AuthzHandler {
	return &authzHandler{signup: signup, signin: signin}
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

	if err := h.signup.SignUp(c.Request().Context(), user); err != nil {
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
