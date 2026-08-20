package api

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

type ItemResponse struct {
	ID              pgtype.UUID       `json:"id"`
	OrganizationID  pgtype.UUID       `json:"organization_id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Type            domain.ItemType   `json:"type"`
	Rate            string            `json:"rate"`
	Currency        string            `json:"currency"`
	IncomeAccount   string            `json:"income_account"`
	TrackInventory  bool              `json:"track_inventory"`
	QuantityInStock int               `json:"quantity_in_stock"`
	Taxes           []ItemTaxResponse `json:"taxes"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type ItemTaxResponse struct {
	ID        pgtype.UUID `json:"id"`
	Name      string      `json:"name"`
	TaxNumber string      `json:"tax_number"`
	Rate      string      `json:"rate"`
	Enabled   bool        `json:"enabled"`
}

func NewItemResponse(item *domain.Item) ItemResponse {
	return ItemResponse{
		ID:              item.ID,
		OrganizationID:  item.OrganizationID,
		Name:            item.Name,
		Description:     item.Description,
		Type:            item.Type,
		Rate:            item.Rate.String(),
		Currency:        item.Currency,
		IncomeAccount:   item.IncomeAccount,
		TrackInventory:  item.TrackInventory,
		QuantityInStock: item.QuantityInStock,
		Taxes:           newItemTaxResponses(item.Taxes),
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

func NewItemResponses(items []*domain.Item) []ItemResponse {
	responses := make([]ItemResponse, 0, len(items))

	for _, item := range items {
		responses = append(responses, NewItemResponse(item))
	}

	return responses
}

func newItemTaxResponses(taxes []domain.ItemTax) []ItemTaxResponse {
	responses := make([]ItemTaxResponse, 0, len(taxes))

	for _, tax := range taxes {
		responses = append(responses, ItemTaxResponse{
			ID:        tax.ID,
			Name:      tax.Name,
			TaxNumber: tax.TaxNumber,
			Rate:      tax.Rate.String(),
			Enabled:   tax.Enabled,
		})
	}

	return responses
}
