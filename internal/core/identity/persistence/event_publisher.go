package persistence

import (
	"context"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

const identityEventsTopic = "identity.events"

type eventProducer interface {
	Publish(ctx context.Context, topic, key string, payload any) error
}

type EventPublisher struct {
	producer eventProducer
}

func NewEventPublisher(producer eventProducer) *EventPublisher {
	return &EventPublisher{producer: producer}
}

func (p *EventPublisher) PublishEmailConfirmationRequested(ctx context.Context, event domain.EmailConfirmationRequested) error {
	return p.producer.Publish(ctx, identityEventsTopic, event.UserID, event)
}

func (p *EventPublisher) PublishPasswordResetRequested(ctx context.Context, event domain.PasswordResetRequested) error {
	return p.producer.Publish(ctx, identityEventsTopic, event.UserID, event)
}

var (
	_ domain.ConfirmationEventPublisher  = (*EventPublisher)(nil)
	_ domain.PasswordResetEventPublisher = (*EventPublisher)(nil)
)
