package domain

import (
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
)

type ItemType string

const (
	ItemTypeItem    ItemType = "ITEM"
	ItemTypeService ItemType = "SERVICE"
)

func (t ItemType) Valid() bool {
	switch t {
	case ItemTypeItem, ItemTypeService:
		return true
	default:
		return false
	}
}

var maxTaxRate = decimal.NewFromInt(100)

type Item struct {
	ID              pgtype.UUID     `json:"id"`
	OrganizationID  pgtype.UUID     `json:"organization_id"`
	Name            string          `json:"name" validate:"required"`
	Description     string          `json:"description"`
	Type            ItemType        `json:"type"`
	Rate            decimal.Decimal `json:"rate"`
	Currency        string          `json:"currency"`
	IncomeAccount   string          `json:"income_account"`
	TrackInventory  bool            `json:"track_inventory"`
	QuantityInStock int             `json:"quantity_in_stock"`
	Taxes           []ItemTax       `json:"taxes"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DeletedAt       *time.Time      `json:"-"`
}

type ItemTax struct {
	ID        pgtype.UUID     `json:"id"`
	ItemID    pgtype.UUID     `json:"item_id"`
	Name      string          `json:"name"`
	TaxNumber string          `json:"tax_number"`
	Rate      decimal.Decimal `json:"rate"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (i *Item) GetAttributes() []interface{} {
	return []interface{}{
		i.OrganizationID, i.Name, i.Description, i.Type, decimalToNumeric(i.Rate),
		i.Currency, i.IncomeAccount, i.TrackInventory, i.QuantityInStock,
	}
}

func (t *ItemTax) GetAttributes() []interface{} {
	return []interface{}{t.ItemID, t.Name, t.TaxNumber, decimalToNumeric(t.Rate), t.Enabled}
}

func (i *Item) IsDeleted() bool {
	return i.DeletedAt != nil
}

func (i *Item) TracksInventory() bool {
	return i.Type == ItemTypeItem && i.TrackInventory
}

func (i *Item) Validate() error {
	if !i.Type.Valid() {
		return ErrInvalidItemType
	}

	i.normalizeForType()

	if strings.TrimSpace(i.Name) == "" {
		return ErrNameRequired
	}

	if i.Rate.Sign() <= 0 {
		return ErrInvalidRate
	}

	if i.TracksInventory() && i.QuantityInStock < 0 {
		return ErrInvalidStockQuantity
	}

	for _, tax := range i.Taxes {
		if err := tax.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (i *Item) normalizeForType() {
	if i.Type != ItemTypeService {
		return
	}

	i.TrackInventory = false
	i.QuantityInStock = 0
}

func (t ItemTax) Validate() error {
	if t.Rate.Sign() < 0 || t.Rate.GreaterThan(maxTaxRate) {
		return ErrInvalidTaxRate
	}

	return nil
}

func decimalToNumeric(d decimal.Decimal) pgtype.Numeric {
	return pgtype.Numeric{Int: d.Coefficient(), Exp: d.Exponent(), Valid: true}
}
