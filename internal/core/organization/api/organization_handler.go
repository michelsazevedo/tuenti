package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/organization/application"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type OrganizationHandler interface {
	GetByID(c echo.Context) error
}

type organizationHandler struct {
	getOrganizationByID application.GetOrganizationByIDUseCase
}

func NewOrganizationHandler(getOrganizationByID application.GetOrganizationByIDUseCase) OrganizationHandler {
	return &organizationHandler{getOrganizationByID: getOrganizationByID}
}

func (h *organizationHandler) GetByID(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid organization id")
	}

	org, err := h.getOrganizationByID.GetByID(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrOrganizationNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "organization not found")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, NewOrganizationResponse(org))
}
