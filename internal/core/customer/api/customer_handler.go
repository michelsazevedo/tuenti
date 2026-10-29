package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/customer/application"
	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type CustomerHandler interface {
	Create(c echo.Context) error
	Get(c echo.Context) error
	List(c echo.Context) error
	Update(c echo.Context) error
	Delete(c echo.Context) error
}

type customerHandler struct {
	createCustomer application.CreateCustomerUseCase
	getCustomer    application.GetCustomerUseCase
	listCustomers  application.ListCustomersUseCase
	updateCustomer *application.UpdateCustomer
	deleteCustomer *application.DeleteCustomer
}

func NewCustomerHandler(
	createCustomer application.CreateCustomerUseCase,
	getCustomer application.GetCustomerUseCase,
	listCustomers application.ListCustomersUseCase,
	updateCustomer *application.UpdateCustomer,
	deleteCustomer *application.DeleteCustomer,
) CustomerHandler {
	return &customerHandler{
		createCustomer: createCustomer,
		getCustomer:    getCustomer,
		listCustomers:  listCustomers,
		updateCustomer: updateCustomer,
		deleteCustomer: deleteCustomer,
	}
}

func (h *customerHandler) Create(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	req := new(CreateCustomerRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, msgInvalidBody)
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	created, err := h.createCustomer.CreateCustomer(c.Request().Context(), req.toDomain(organizationID))
	if err != nil {
		return h.failure(c, err, "create")
	}

	return c.JSON(http.StatusCreated, NewCustomerResponse(created))
}

func (h *customerHandler) Get(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	id, err := customerID(c)
	if err != nil {
		return err
	}

	customer, err := h.getCustomer.GetCustomer(c.Request().Context(), organizationID, id)
	if err != nil {
		return h.failure(c, err, "get")
	}

	return c.JSON(http.StatusOK, NewCustomerResponse(customer))
}

func (h *customerHandler) List(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	filter, err := listCustomersFilter(c, organizationID)
	if err != nil {
		return err
	}

	customers, err := h.listCustomers.ListCustomers(c.Request().Context(), filter)
	if err != nil {
		return h.failure(c, err, "list")
	}

	return c.JSON(http.StatusOK, NewCustomerResponses(customers))
}

func (h *customerHandler) Update(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	id, err := customerID(c)
	if err != nil {
		return err
	}

	req := new(UpdateCustomerRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, msgInvalidBody)
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	customer := req.toDomain(organizationID)
	customer.ID = id

	updated, err := h.updateCustomer.UpdateCustomer(c.Request().Context(), customer)
	if err != nil {
		return h.failure(c, err, "update")
	}

	return c.JSON(http.StatusOK, NewCustomerResponse(updated))
}

func (h *customerHandler) Delete(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	id, err := customerID(c)
	if err != nil {
		return err
	}

	if err := h.deleteCustomer.DeleteCustomer(c.Request().Context(), organizationID, id); err != nil {
		return h.failure(c, err, "delete")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *customerHandler) authenticatedOrganization(c echo.Context) (pgtype.UUID, error) {
	identity, err := m.IdentityFromContext(c.Request().Context())
	if err != nil {
		logger := observability.Logger(c.Request().Context())

		logger.Error().Err(err).
			Str("event", "customer_handler_missing_identity").
			Str("path", c.Path()).
			Msg("customer handler ran without an authenticated organization, RequireAuth must run before it")

		return pgtype.UUID{}, echo.NewHTTPError(http.StatusInternalServerError, msgInternalServerError)
	}

	return identity.CurrentOrganizationID, nil
}

func customerID(c echo.Context) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return pgtype.UUID{}, echo.NewHTTPError(http.StatusBadRequest, msgInvalidCustomerID)
	}

	return id, nil
}

func listCustomersFilter(c echo.Context, organizationID pgtype.UUID) (domain.ListCustomersFilter, error) {
	search := c.QueryParam("search")
	if containsNUL(search) {
		return domain.ListCustomersFilter{}, echo.NewHTTPError(http.StatusBadRequest, msgInvalidSearch)
	}

	return domain.ListCustomersFilter{
		OrganizationID: organizationID,
		Search:         strings.TrimSpace(search),
	}, nil
}

func (h *customerHandler) failure(c echo.Context, err error, event string) error {
	if errors.Is(err, domain.ErrCustomerNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, domain.ErrCustomerNotFound.Error())
	}

	if verr, ok := errors.AsType[domain.ValidationError](err); ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, verr.Error())
	}

	logger := observability.Logger(c.Request().Context())

	logger.Error().Err(err).
		Str("event", "customer_handler_"+event+"_failed").
		Str("path", c.Path()).
		Msg("customer request failed, responding 500")

	return echo.NewHTTPError(http.StatusInternalServerError, msgInternalServerError)
}

const (
	msgInvalidBody = "invalid request body"

	msgInvalidCustomerID = "invalid customer id"

	msgInvalidSearch = "invalid search term"

	msgInternalServerError = "internal server error"
)
