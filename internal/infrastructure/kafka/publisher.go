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
