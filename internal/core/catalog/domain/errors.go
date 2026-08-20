package domain

import "errors"

var (
	ErrItemNotFound         = errors.New("item not found")
	ErrInvalidItemType      = errors.New("invalid item type")
	ErrNameRequired         = errors.New("name required")
	ErrInvalidRate          = errors.New("invalid rate")
	ErrInvalidTaxRate       = errors.New("invalid tax rate")
	ErrInvalidStockQuantity = errors.New("invalid stock quantity")
)
