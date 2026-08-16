package persistence

import (
	"context"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const organizationEventsTopic = "organization.events"

type eventProducer interface {
	Publish(ctx context.Context, topic, key string, payload any) error
}

type EventPublisher struct {
	producer eventProducer
}

func NewEventPublisher(producer eventProducer) *EventPublisher {
	return &EventPublisher{producer: producer}
}

func (p *EventPublisher) PublishOrganizationInvitationCreated(ctx context.Context, event domain.OrganizationInvitationCreated) error {
	return p.producer.Publish(ctx, organizationEventsTopic, event.InvitationID, event)
}

var _ domain.InvitationEventPublisher = (*EventPublisher)(nil)
