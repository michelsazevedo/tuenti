package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type recordedEventPublish struct {
	topic, key string
	payload    any
}

type recordingEventProducer struct {
	calls []recordedEventPublish
	err   error
}

func (r *recordingEventProducer) Publish(_ context.Context, topic, key string, payload any) error {
	r.calls = append(r.calls, recordedEventPublish{topic: topic, key: key, payload: payload})

	return r.err
}

func TestEventPublisherPublishesOrganizationInvitationCreatedToTheOrganizationTopic(t *testing.T) {
	producer := &recordingEventProducer{}
	event := domain.NewOrganizationInvitationCreated(
		"9c1e7a44-3b2d-4e5f-8a6b-7c8d9e0f1a2b", "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d", "Acme Corp",
		"5f8d0d55-1c2b-4d3e-9a7f-6b1c2d3e4f50", "Wile E. Coyote", "road.runner@example.com", "member",
		"https://tuenti.test/invitations/accept?token=acme",
	)

	require.NoError(t, NewEventPublisher(producer).PublishOrganizationInvitationCreated(context.Background(), event))

	require.Len(t, producer.calls, 1, "the publisher must emit exactly one message")
	assert.Equal(t, organizationEventsTopic, producer.calls[0].topic,
		"organization events must go to the organization topic")
	assert.Equal(t, event.InvitationID, producer.calls[0].key,
		"the partition key must keep an invitation's events ordered")
	assert.Equal(t, event, producer.calls[0].payload)
}

func TestEventPublisherPropagatesProducerFailures(t *testing.T) {
	sentinel := errors.New("broker unavailable")
	producer := &recordingEventProducer{err: sentinel}

	event := domain.NewOrganizationInvitationCreated(
		"9c1e7a44-3b2d-4e5f-8a6b-7c8d9e0f1a2b", "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d", "Acme Corp",
		"5f8d0d55-1c2b-4d3e-9a7f-6b1c2d3e4f50", "Wile E. Coyote", "road.runner@example.com", "member",
		"https://tuenti.test/invitations/accept?token=acme",
	)

	err := NewEventPublisher(producer).PublishOrganizationInvitationCreated(context.Background(), event)

	require.Error(t, err, "a publish failure must reach the caller, not be swallowed here")
	assert.ErrorIs(t, err, sentinel, "the underlying client error must stay unwrappable")
}
