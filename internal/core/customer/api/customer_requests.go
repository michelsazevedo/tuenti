package api

import (
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

var validate = newValidator()

// tagNoNUL rejects strings carrying a NUL byte. Postgres refuses NUL in text
// columns (SQLSTATE 22021), so letting one through turns a malformed request
// into a 500 instead of a 422.
const tagNoNUL = "nonul"

func newValidator() *validator.Validate {
	v := validator.New()

	if err := v.RegisterValidation(tagNoNUL, func(fl validator.FieldLevel) bool {
		return !containsNUL(fl.Field().String())
	}); err != nil {
		panic("customer api: registering the " + tagNoNUL + " validation: " + err.Error())
	}

	return v
}

// containsNUL reports whether s carries a NUL byte, which Postgres refuses in text.
func containsNUL(s string) bool {
	return strings.ContainsRune(s, 0)
}

// AddressRequest is optional as a whole and field by field. The country code
// is validated (and normalized) by the domain, not at the binding layer; the
// max= tags only mirror the column lengths in the customers table.
type AddressRequest struct {
	Street     string `json:"street" validate:"max=255,nonul"`
	Complement string `json:"complement" validate:"max=255,nonul"`
	City       string `json:"city" validate:"max=255,nonul"`
	State      string `json:"state" validate:"max=255,nonul"`
	Country    string `json:"country" validate:"max=2,nonul"`
}

type CreateCustomerRequest struct {
	Name        string          `json:"name" validate:"required,max=255,nonul"`
	CompanyName string          `json:"company_name" validate:"required,max=255,nonul"`
	Email       string          `json:"email" validate:"required,max=255,nonul,email"`
	PhoneNumber string          `json:"phone_number" validate:"required,max=50,nonul"`
	Address     *AddressRequest `json:"address" validate:"omitempty"`
}

func (r *CreateCustomerRequest) Validate() error {
	return validate.Struct(r)
}

type UpdateCustomerRequest struct {
	CreateCustomerRequest
}

func (r *UpdateCustomerRequest) Validate() error {
	return validate.Struct(r)
}

func (r *CreateCustomerRequest) toDomain(organizationID pgtype.UUID) *domain.Customer {
	return &domain.Customer{
		OrganizationID: organizationID,
		Name:           r.Name,
		CompanyName:    r.CompanyName,
		Email:          r.Email,
		PhoneNumber:    r.PhoneNumber,
		Address:        r.Address.toDomain(),
	}
}

func (r *AddressRequest) toDomain() *domain.Address {
	if r == nil {
		return nil
	}

	return &domain.Address{
		Street:     r.Street,
		Complement: r.Complement,
		City:       r.City,
		State:      r.State,
		Country:    r.Country,
	}
}
