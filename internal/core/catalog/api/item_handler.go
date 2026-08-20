package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/application"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type ItemHandler interface {
	Create(c echo.Context) error
	Get(c echo.Context) error
	List(c echo.Context) error
	Update(c echo.Context) error
	Delete(c echo.Context) error
}

type itemHandler struct {
	createItem application.CreateItemUseCase
	getItem    application.GetItemUseCase
	listItems  application.ListItemsUseCase
	updateItem *application.UpdateItem
	deleteItem *application.DeleteItem
}

func NewItemHandler(
	createItem application.CreateItemUseCase,
	getItem application.GetItemUseCase,
	listItems application.ListItemsUseCase,
	updateItem *application.UpdateItem,
	deleteItem *application.DeleteItem,
) ItemHandler {
	return &itemHandler{
		createItem: createItem,
		getItem:    getItem,
		listItems:  listItems,
		updateItem: updateItem,
		deleteItem: deleteItem,
	}
}

func (h *itemHandler) Create(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	req := new(CreateItemRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, msgInvalidBody)
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	item, err := req.toDomain(organizationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	created, err := h.createItem.CreateItem(c.Request().Context(), item)
	if err != nil {
		return h.failure(c, err, "create")
	}

	return c.JSON(http.StatusCreated, NewItemResponse(created))
}

func (h *itemHandler) Get(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	id, err := itemID(c)
	if err != nil {
		return err
	}

	item, err := h.getItem.GetItem(c.Request().Context(), organizationID, id)
	if err != nil {
		return h.failure(c, err, "get")
	}

	return c.JSON(http.StatusOK, NewItemResponse(item))
}

func (h *itemHandler) List(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	filter, err := listItemsFilter(c, organizationID)
	if err != nil {
		return err
	}

	items, err := h.listItems.ListItems(c.Request().Context(), filter)
	if err != nil {
		return h.failure(c, err, "list")
	}

	return c.JSON(http.StatusOK, NewItemResponses(items))
}

func (h *itemHandler) Update(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	id, err := itemID(c)
	if err != nil {
		return err
	}

	req := new(UpdateItemRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, msgInvalidBody)
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	item, err := req.toDomain(organizationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	item.ID = id

	updated, err := h.updateItem.UpdateItem(c.Request().Context(), item)
	if err != nil {
		return h.failure(c, err, "update")
	}

	return c.JSON(http.StatusOK, NewItemResponse(updated))
}

func (h *itemHandler) Delete(c echo.Context) error {
	organizationID, err := h.authenticatedOrganization(c)
	if err != nil {
		return err
	}

	id, err := itemID(c)
	if err != nil {
		return err
	}

	if err := h.deleteItem.DeleteItem(c.Request().Context(), organizationID, id); err != nil {
		return h.failure(c, err, "delete")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *itemHandler) authenticatedOrganization(c echo.Context) (pgtype.UUID, error) {
	identity, err := m.IdentityFromContext(c.Request().Context())
	if err != nil {
		logger := observability.Logger(c.Request().Context())

		logger.Error().Err(err).
			Str("event", "item_handler_missing_identity").
			Str("path", c.Path()).
			Msg("item handler ran without an authenticated organization, RequireAuth must run before it")

		return pgtype.UUID{}, echo.NewHTTPError(http.StatusInternalServerError, msgInternalServerError)
	}

	return identity.CurrentOrganizationID, nil
}

func itemID(c echo.Context) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return pgtype.UUID{}, echo.NewHTTPError(http.StatusBadRequest, msgInvalidItemID)
	}

	return id, nil
}

func listItemsFilter(c echo.Context, organizationID pgtype.UUID) (domain.ListItemsFilter, error) {
	filter := domain.ListItemsFilter{
		OrganizationID: organizationID,
		Search:         strings.TrimSpace(c.QueryParam("search")),
	}

	raw := strings.TrimSpace(c.QueryParam("type"))
	if raw == "" {
		return filter, nil
	}

	itemType := domain.ItemType(raw)
	if !itemType.Valid() {
		return domain.ListItemsFilter{},
			echo.NewHTTPError(http.StatusUnprocessableEntity, domain.ErrInvalidItemType.Error())
	}

	filter.Type = &itemType

	return filter, nil
}

func (h *itemHandler) failure(c echo.Context, err error, event string) error {
	if errors.Is(err, domain.ErrItemNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, domain.ErrItemNotFound.Error())
	}

	if verr, ok := errors.AsType[domain.ValidationError](err); ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, verr.Error())
	}

	logger := observability.Logger(c.Request().Context())

	logger.Error().Err(err).
		Str("event", "item_handler_"+event+"_failed").
		Str("path", c.Path()).
		Msg("catalog request failed, responding 500")

	return echo.NewHTTPError(http.StatusInternalServerError, msgInternalServerError)
}

const (
	msgInvalidBody = "invalid request body"

	msgInvalidItemID = "invalid item id"

	msgInternalServerError = "internal server error"
)
