package kafka

import (
	"context"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const (
	identityEventsTopic     = "identity.events"
	organizationEventsTopic = "organization.events"
)

// EventPublisher adapts the low level Producer to the publisher interfaces owned
// by each bounded context. It holds no state of its own: every method maps a
// domain event onto a topic and a partition key, and delegates the encoding and
// the write to the Producer. Publish failures are deliberately returned as-is —
// the best-effort logging policy belongs to the application layer.
type EventPublisher struct {
	producer *Producer
}

func NewEventPublisher(producer *Producer) *EventPublisher {
	return &EventPublisher{producer: producer}
}

func (p *EventPublisher) PublishEmailConfirmationRequested(ctx context.Context, event identitydomain.EmailConfirmationRequested) error {
	return p.producer.Publish(ctx, identityEventsTopic, event.UserID, event)
}

func (p *EventPublisher) PublishPasswordResetRequested(ctx context.Context, event identitydomain.PasswordResetRequested) error {
	return p.producer.Publish(ctx, identityEventsTopic, event.UserID, event)
}

func (p *EventPublisher) PublishOrganizationInvitationCreated(ctx context.Context, event orgdomain.OrganizationInvitationCreated) error {
	return p.producer.Publish(ctx, organizationEventsTopic, event.InvitationID, event)
}

var (
	_ identitydomain.ConfirmationEventPublisher  = (*EventPublisher)(nil)
	_ identitydomain.PasswordResetEventPublisher = (*EventPublisher)(nil)
	_ orgdomain.InvitationEventPublisher         = (*EventPublisher)(nil)
)
