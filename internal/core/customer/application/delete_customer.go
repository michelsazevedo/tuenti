package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
)

type DeleteCustomer struct {
	repo domain.CustomerRepository
}

func NewDeleteCustomer(repo domain.CustomerRepository) *DeleteCustomer {
	return &DeleteCustomer{repo: repo}
}

func (uc *DeleteCustomer) DeleteCustomer(ctx context.Context, organizationID, id pgtype.UUID) error {
	if err := uc.repo.Delete(ctx, organizationID, id); err != nil {
		return err
	}

	logCustomerEvent(ctx, eventCustomerDeleted, "Customer deleted", organizationID, id)

	return nil
}
