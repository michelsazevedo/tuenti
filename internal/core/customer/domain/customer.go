package domain

import (
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type Customer struct {
	ID             pgtype.UUID `json:"id"`
	OrganizationID pgtype.UUID `json:"organization_id"`
	Name           string      `json:"name"`
	CompanyName    string      `json:"company_name"`
	Email          string      `json:"email"`
	PhoneNumber    string      `json:"phone_number"`
	Address        *Address    `json:"address"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	DeletedAt      *time.Time  `json:"-"`
}

type Address struct {
	Street     string `json:"street"`
	Complement string `json:"complement"`
	City       string `json:"city"`
	State      string `json:"state"`
	Country    string `json:"country"`
}

func (c *Customer) IsDeleted() bool {
	return c.DeletedAt != nil
}

// Validate checks the customer's invariants. On success, a present
// Address.Country is normalized in place to its uppercase ISO 3166-1 alpha-2 form.
func (c *Customer) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return ErrNameRequired
	}

	if strings.TrimSpace(c.CompanyName) == "" {
		return ErrCompanyNameRequired
	}

	if !isValidEmail(c.Email) {
		return ErrInvalidEmail
	}

	if strings.TrimSpace(c.PhoneNumber) == "" {
		return ErrPhoneNumberRequired
	}

	if c.Address != nil {
		return c.Address.normalizeCountry()
	}

	return nil
}

func (a *Address) normalizeCountry() error {
	country := strings.TrimSpace(a.Country)
	if country == "" {
		return nil
	}

	code := strings.ToUpper(country)
	if _, ok := isoCountryCodes[code]; !ok {
		return ErrInvalidCountry
	}

	a.Country = code

	return nil
}

// isValidEmail accepts only a bare addr-spec ("user@example.com"); display-name
// forms such as "Jane <jane@example.com>" and surrounding whitespace are rejected.
func isValidEmail(email string) bool {
	if email == "" {
		return false
	}

	addr, err := mail.ParseAddress(email)

	return err == nil && addr.Address == email
}
