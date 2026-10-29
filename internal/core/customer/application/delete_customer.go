package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
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

	logCustomerDeleted(ctx, organizationID, id)

	return nil
}

func logCustomerDeleted(ctx context.Context, organizationID, id pgtype.UUID) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", "customer_deleted").
		Str("customer_id", id.String()).
		Str("organization_id", organizationID.String()).
		Msg("Customer deleted")
}
