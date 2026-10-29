package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validCustomer() Customer {
	return Customer{
		Name:        "Jane Doe",
		CompanyName: "Acme Inc",
		Email:       "jane@acme.com",
		PhoneNumber: "+1 555 0100",
	}
}

func TestCustomerValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Customer)
		want   error
	}{
		{name: "valid customer without address", mutate: func(c *Customer) {}},
		{
			name: "valid customer with full address",
			mutate: func(c *Customer) {
				c.Address = &Address{Street: "1 Main St", Complement: "Apt 2", City: "Austin", State: "TX", Country: "US"}
			},
		},
		{
			name:   "valid customer with address but empty country",
			mutate: func(c *Customer) { c.Address = &Address{Street: "1 Main St", City: "Austin"} },
		},
		{
			name:   "valid customer with address but blank country",
			mutate: func(c *Customer) { c.Address = &Address{City: "Austin", Country: "   "} },
		},
		{name: "empty name", mutate: func(c *Customer) { c.Name = "" }, want: ErrNameRequired},
		{name: "blank name", mutate: func(c *Customer) { c.Name = "   " }, want: ErrNameRequired},
		{name: "empty company name", mutate: func(c *Customer) { c.CompanyName = "" }, want: ErrCompanyNameRequired},
		{name: "blank company name", mutate: func(c *Customer) { c.CompanyName = " \t" }, want: ErrCompanyNameRequired},
		{name: "empty email", mutate: func(c *Customer) { c.Email = "" }, want: ErrInvalidEmail},
		{name: "blank email", mutate: func(c *Customer) { c.Email = "   " }, want: ErrInvalidEmail},
		{name: "email without at sign", mutate: func(c *Customer) { c.Email = "jane.acme.com" }, want: ErrInvalidEmail},
		{name: "email without domain", mutate: func(c *Customer) { c.Email = "jane@" }, want: ErrInvalidEmail},
		{name: "email without local part", mutate: func(c *Customer) { c.Email = "@acme.com" }, want: ErrInvalidEmail},
		{name: "email with display name", mutate: func(c *Customer) { c.Email = "Jane <jane@acme.com>" }, want: ErrInvalidEmail},
		{name: "email with surrounding whitespace", mutate: func(c *Customer) { c.Email = " jane@acme.com " }, want: ErrInvalidEmail},
		{name: "empty phone number", mutate: func(c *Customer) { c.PhoneNumber = "" }, want: ErrPhoneNumberRequired},
		{name: "blank phone number", mutate: func(c *Customer) { c.PhoneNumber = "  " }, want: ErrPhoneNumberRequired},
		{
			name:   "unknown country code",
			mutate: func(c *Customer) { c.Address = &Address{Country: "XX"} },
			want:   ErrInvalidCountry,
		},
		{
			name:   "alpha-3 country code",
			mutate: func(c *Customer) { c.Address = &Address{Country: "USA"} },
			want:   ErrInvalidCountry,
		},
		{
			name:   "country name instead of code",
			mutate: func(c *Customer) { c.Address = &Address{Country: "Brazil"} },
			want:   ErrInvalidCountry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customer := validCustomer()
			tt.mutate(&customer)

			assert.ErrorIs(t, customer.Validate(), tt.want)
		})
	}
}

func TestCustomerValidateErrorsAreValidationErrors(t *testing.T) {
	for _, err := range []error{
		ErrCustomerEmailTaken, ErrNameRequired, ErrCompanyNameRequired,
		ErrInvalidEmail, ErrPhoneNumberRequired, ErrInvalidCountry,
	} {
		var validationErr ValidationError
		assert.ErrorAs(t, err, &validationErr, err.Error())
	}

	var validationErr ValidationError
	assert.NotErrorAs(t, ErrCustomerNotFound, &validationErr)
}

func TestCustomerValidateNormalizesCountry(t *testing.T) {
	tests := []struct {
		name    string
		country string
		want    string
	}{
		{name: "lowercase", country: "br", want: "BR"},
		{name: "mixed case", country: "gB", want: "GB"},
		{name: "surrounding whitespace", country: " de ", want: "DE"},
		{name: "already uppercase", country: "JP", want: "JP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			customer := validCustomer()
			customer.Address = &Address{City: "Somewhere", Country: tt.country}

			require.NoError(t, customer.Validate())

			assert.Equal(t, tt.want, customer.Address.Country)
		})
	}
}

func TestCustomerValidateLeavesInvalidCountryUntouched(t *testing.T) {
	customer := validCustomer()
	customer.Address = &Address{Country: "xx"}

	require.ErrorIs(t, customer.Validate(), ErrInvalidCountry)

	assert.Equal(t, "xx", customer.Address.Country)
}

func TestISOCountryCodes(t *testing.T) {
	assert.Len(t, isoCountryCodes, 249)

	for code := range isoCountryCodes {
		assert.Len(t, code, 2, code)
		assert.Regexp(t, `^[A-Z]{2}$`, code)
	}
}

func TestCustomerIsDeleted(t *testing.T) {
	deletedAt := time.Now()

	assert.False(t, (&Customer{}).IsDeleted())
	assert.True(t, (&Customer{DeletedAt: &deletedAt}).IsDeleted())
}
