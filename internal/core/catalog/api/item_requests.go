package api

import (
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

var validate = validator.New()

var (
	errInvalidDecimal = errors.New("invalid decimal value")

	errInvalidTaxID = errors.New("invalid tax id")
)

type ItemTaxRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name" validate:"required"`
	TaxNumber string `json:"tax_number"`
	Rate      string `json:"rate" validate:"required"`
	Enabled   bool   `json:"enabled"`
}

type CreateItemRequest struct {
	Name            string           `json:"name" validate:"required"`
	Description     string           `json:"description"`
	Type            string           `json:"type" validate:"required,oneof=ITEM SERVICE"`
	Rate            string           `json:"rate" validate:"required"`
	Currency        string           `json:"currency" validate:"required"`
	IncomeAccount   string           `json:"income_account" validate:"required"`
	TrackInventory  bool             `json:"track_inventory"`
	QuantityInStock int              `json:"quantity_in_stock" validate:"gte=0"`
	Taxes           []ItemTaxRequest `json:"taxes" validate:"dive"`
}

func (r *CreateItemRequest) Validate() error {
	return validate.Struct(r)
}

type UpdateItemRequest struct {
	CreateItemRequest
}

func (r *UpdateItemRequest) Validate() error {
	return validate.Struct(r)
}

func (r *CreateItemRequest) toDomain(organizationID pgtype.UUID) (*domain.Item, error) {
	rate, err := parseDecimal("rate", r.Rate)
	if err != nil {
		return nil, err
	}

	taxes, err := toDomainTaxes(r.Taxes)
	if err != nil {
		return nil, err
	}

	return &domain.Item{
		OrganizationID:  organizationID,
		Name:            r.Name,
		Description:     r.Description,
		Type:            domain.ItemType(r.Type),
		Rate:            rate,
		Currency:        r.Currency,
		IncomeAccount:   r.IncomeAccount,
		TrackInventory:  r.TrackInventory,
		QuantityInStock: r.QuantityInStock,
		Taxes:           taxes,
	}, nil
}

func toDomainTaxes(requests []ItemTaxRequest) ([]domain.ItemTax, error) {
	taxes := make([]domain.ItemTax, 0, len(requests))

	for _, request := range requests {
		tax, err := request.toDomain()
		if err != nil {
			return nil, err
		}

		taxes = append(taxes, tax)
	}

	return taxes, nil
}

func (r ItemTaxRequest) toDomain() (domain.ItemTax, error) {
	rate, err := parseDecimal("tax rate", r.Rate)
	if err != nil {
		return domain.ItemTax{}, err
	}

	tax := domain.ItemTax{
		Name:      r.Name,
		TaxNumber: r.TaxNumber,
		Rate:      rate,
		Enabled:   r.Enabled,
	}

	if r.ID == "" {
		return tax, nil
	}

	if err := tax.ID.Scan(r.ID); err != nil {
		return domain.ItemTax{}, errInvalidTaxID
	}

	return tax, nil
}

func parseDecimal(field, raw string) (decimal.Decimal, error) {
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("%w: %s", errInvalidDecimal, field)
	}

	return value, nil
}
