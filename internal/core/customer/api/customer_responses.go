package api

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

// CustomerResponse never exposes deleted_at: a soft-deleted customer is not
// served at all. A customer without an address serialises "address": null so
// the response shape stays stable for clients.
type CustomerResponse struct {
	ID             pgtype.UUID      `json:"id"`
	OrganizationID pgtype.UUID      `json:"organization_id"`
	Name           string           `json:"name"`
	CompanyName    string           `json:"company_name"`
	Email          string           `json:"email"`
	PhoneNumber    string           `json:"phone_number"`
	Address        *AddressResponse `json:"address"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type AddressResponse struct {
	Street     string `json:"street"`
	Complement string `json:"complement"`
	City       string `json:"city"`
	State      string `json:"state"`
	Country    string `json:"country"`
}

func NewCustomerResponse(customer *domain.Customer) CustomerResponse {
	return CustomerResponse{
		ID:             customer.ID,
		OrganizationID: customer.OrganizationID,
		Name:           customer.Name,
		CompanyName:    customer.CompanyName,
		Email:          customer.Email,
		PhoneNumber:    customer.PhoneNumber,
		Address:        newAddressResponse(customer.Address),
		CreatedAt:      customer.CreatedAt,
		UpdatedAt:      customer.UpdatedAt,
	}
}

func NewCustomerResponses(customers []*domain.Customer) []CustomerResponse {
	responses := make([]CustomerResponse, 0, len(customers))

	for _, customer := range customers {
		responses = append(responses, NewCustomerResponse(customer))
	}

	return responses
}

func newAddressResponse(address *domain.Address) *AddressResponse {
	if address == nil {
		return nil
	}

	return &AddressResponse{
		Street:     address.Street,
		Complement: address.Complement,
		City:       address.City,
		State:      address.State,
		Country:    address.Country,
	}
}
