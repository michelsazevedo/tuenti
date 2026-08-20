package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestItemTypeValid(t *testing.T) {
	tests := []struct {
		name     string
		itemType ItemType
		want     bool
	}{
		{name: "item", itemType: ItemTypeItem, want: true},
		{name: "service", itemType: ItemTypeService, want: true},
		{name: "unknown type", itemType: ItemType("BUNDLE"), want: false},
		{name: "empty type", itemType: ItemType(""), want: false},
		{name: "wrong case", itemType: ItemType("Item"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.itemType.Valid())
		})
	}
}

func TestItemTaxValidate(t *testing.T) {
	tests := []struct {
		name string
		rate decimal.Decimal
		want error
	}{
		{name: "zero rate", rate: decimal.Zero},
		{name: "fractional rate", rate: decimal.RequireFromString("7.25")},
		{name: "upper bound", rate: decimal.NewFromInt(100)},
		{name: "negative rate", rate: decimal.NewFromInt(-1), want: ErrInvalidTaxRate},
		{name: "above upper bound", rate: decimal.RequireFromString("100.01"), want: ErrInvalidTaxRate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tax := ItemTax{Name: "VAT", Rate: tt.rate}

			assert.ErrorIs(t, tax.Validate(), tt.want)
		})
	}
}

func TestItemValidate(t *testing.T) {
	tests := []struct {
		name string
		item Item
		want error
	}{
		{
			name: "valid item without inventory tracking",
			item: Item{Name: "Monitor", Type: ItemTypeItem, Rate: decimal.NewFromInt(150)},
		},
		{
			name: "valid item tracking inventory",
			item: Item{
				Name:            "Monitor",
				Type:            ItemTypeItem,
				Rate:            decimal.NewFromInt(150),
				TrackInventory:  true,
				QuantityInStock: 3,
			},
		},
		{
			name: "valid service",
			item: Item{Name: "Consulting", Type: ItemTypeService, Rate: decimal.RequireFromString("99.99")},
		},
		{
			name: "valid item with taxes",
			item: Item{
				Name:  "Monitor",
				Type:  ItemTypeItem,
				Rate:  decimal.NewFromInt(150),
				Taxes: []ItemTax{{Name: "VAT", Rate: decimal.NewFromInt(21)}},
			},
		},
		{
			name: "unknown type",
			item: Item{Name: "Monitor", Type: ItemType("BUNDLE"), Rate: decimal.NewFromInt(150)},
			want: ErrInvalidItemType,
		},
		{
			name: "empty name",
			item: Item{Type: ItemTypeItem, Rate: decimal.NewFromInt(150)},
			want: ErrNameRequired,
		},
		{
			name: "blank name",
			item: Item{Name: "   ", Type: ItemTypeItem, Rate: decimal.NewFromInt(150)},
			want: ErrNameRequired,
		},
		{
			name: "zero rate",
			item: Item{Name: "Monitor", Type: ItemTypeItem, Rate: decimal.Zero},
			want: ErrInvalidRate,
		},
		{
			name: "negative rate",
			item: Item{Name: "Monitor", Type: ItemTypeItem, Rate: decimal.NewFromInt(-1)},
			want: ErrInvalidRate,
		},
		{
			name: "negative stock while tracking inventory",
			item: Item{
				Name:            "Monitor",
				Type:            ItemTypeItem,
				Rate:            decimal.NewFromInt(150),
				TrackInventory:  true,
				QuantityInStock: -1,
			},
			want: ErrInvalidStockQuantity,
		},
		{
			name: "invalid tax rate",
			item: Item{
				Name:  "Monitor",
				Type:  ItemTypeItem,
				Rate:  decimal.NewFromInt(150),
				Taxes: []ItemTax{{Name: "VAT", Rate: decimal.NewFromInt(21)}, {Name: "Bogus", Rate: decimal.NewFromInt(101)}},
			},
			want: ErrInvalidTaxRate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := tt.item

			assert.ErrorIs(t, item.Validate(), tt.want)
		})
	}
}

func TestItemValidateNormalizesServiceInventory(t *testing.T) {
	item := Item{
		Name:            "Consulting",
		Type:            ItemTypeService,
		Rate:            decimal.NewFromInt(200),
		TrackInventory:  true,
		QuantityInStock: -7,
	}

	require.NoError(t, item.Validate())

	assert.False(t, item.TrackInventory)
	assert.Zero(t, item.QuantityInStock)
	assert.False(t, item.TracksInventory())
}

func TestItemValidatePreservesItemInventory(t *testing.T) {
	item := Item{
		Name:            "Monitor",
		Type:            ItemTypeItem,
		Rate:            decimal.NewFromInt(150),
		TrackInventory:  true,
		QuantityInStock: 12,
	}

	require.NoError(t, item.Validate())

	assert.True(t, item.TrackInventory)
	assert.Equal(t, 12, item.QuantityInStock)
	assert.True(t, item.TracksInventory())
}

func TestItemIsDeleted(t *testing.T) {
	deletedAt := time.Now()

	assert.False(t, (&Item{}).IsDeleted())
	assert.True(t, (&Item{DeletedAt: &deletedAt}).IsDeleted())
}
