package domain

import "errors"

type ValidationError struct{ error }

func newValidationError(msg string) ValidationError {
	return ValidationError{errors.New(msg)}
}

var (
	ErrItemNotFound = errors.New("item not found")

	ErrInvalidItemType      = newValidationError("invalid item type")
	ErrNameRequired         = newValidationError("name required")
	ErrInvalidRate          = newValidationError("invalid rate")
	ErrInvalidTaxRate       = newValidationError("invalid tax rate")
	ErrInvalidStockQuantity = newValidationError("invalid stock quantity")
)
