package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const (
	eventCustomerCreated = "customer_created"
	eventCustomerUpdated = "customer_updated"
	eventCustomerDeleted = "customer_deleted"
)

// logCustomerEvent emits a customer lifecycle event carrying identifiers only.
// Customer records are PII (name, email, phone, address), so the signature
// deliberately accepts IDs rather than *domain.Customer: no caller can leak a
// personal field into the log by adding one more .Str(...).
func logCustomerEvent(ctx context.Context, event, msg string, organizationID, customerID pgtype.UUID) {
	logger := observability.Logger(ctx)

	logger.Info().
		Str("event", event).
		Str("customer_id", customerID.String()).
		Str("organization_id", organizationID.String()).
		Msg(msg)
}
