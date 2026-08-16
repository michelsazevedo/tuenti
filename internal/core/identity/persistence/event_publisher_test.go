package persistence

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
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

func TestEventPublisherPublishesEmailConfirmationRequestedToTheIdentityTopic(t *testing.T) {
	producer := &recordingEventProducer{}
	event := domain.NewEmailConfirmationRequested(
		"5f8d0d55-1c2b-4d3e-9a7f-6b1c2d3e4f50", "Wile E. Coyote", "wile.coyote@example.com",
		"https://tuenti.test/confirm?token=acme",
	)

	require.NoError(t, NewEventPublisher(producer).PublishEmailConfirmationRequested(context.Background(), event))

	require.Len(t, producer.calls, 1, "the publisher must emit exactly one message")
	assert.Equal(t, identityEventsTopic, producer.calls[0].topic, "identity events must go to the identity topic")
	assert.Equal(t, event.UserID, producer.calls[0].key, "the partition key must keep a user's events ordered")
	assert.Equal(t, event, producer.calls[0].payload)
}

func TestEventPublisherPublishesPasswordResetRequestedToTheIdentityTopic(t *testing.T) {
	producer := &recordingEventProducer{}
	event := domain.NewPasswordResetRequested(
		"5f8d0d55-1c2b-4d3e-9a7f-6b1c2d3e4f50", "Wile E. Coyote", "wile.coyote@example.com",
		"https://tuenti.test/reset?token=acme",
	)

	require.NoError(t, NewEventPublisher(producer).PublishPasswordResetRequested(context.Background(), event))

	require.Len(t, producer.calls, 1, "the publisher must emit exactly one message")
	assert.Equal(t, identityEventsTopic, producer.calls[0].topic, "identity events must go to the identity topic")
	assert.Equal(t, event.UserID, producer.calls[0].key, "the partition key must keep a user's events ordered")
	assert.Equal(t, event, producer.calls[0].payload)
}

func TestEventPublisherPropagatesProducerFailures(t *testing.T) {
	sentinel := errors.New("broker unavailable")
	producer := &recordingEventProducer{err: sentinel}

	event := domain.NewEmailConfirmationRequested(
		"5f8d0d55-1c2b-4d3e-9a7f-6b1c2d3e4f50", "Wile E. Coyote", "wile.coyote@example.com",
		"https://tuenti.test/confirm?token=acme",
	)

	err := NewEventPublisher(producer).PublishEmailConfirmationRequested(context.Background(), event)

	require.Error(t, err, "a publish failure must reach the caller, not be swallowed here")
	assert.ErrorIs(t, err, sentinel, "the underlying client error must stay unwrappable")
}
