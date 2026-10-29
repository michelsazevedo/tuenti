package domain

import "errors"

type ValidationError struct{ error }

func newValidationError(msg string) ValidationError {
	return ValidationError{errors.New(msg)}
}

var (
	ErrCustomerNotFound = errors.New("customer not found")

	ErrCustomerEmailTaken  = newValidationError("email already in use")
	ErrNameRequired        = newValidationError("name required")
	ErrCompanyNameRequired = newValidationError("company name required")
	ErrInvalidEmail        = newValidationError("invalid email")
	ErrPhoneNumberRequired = newValidationError("phone number required")
	ErrInvalidCountry      = newValidationError("invalid country")
)
