package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const (
	userID       = "5f8d0d55-1c2b-4d3e-9a7f-6b1c2d3e4f50"
	invitationID = "9c1e7a44-3b2d-4e5f-8a6b-7c8d9e0f1a2b"
)

// publishFunc lets one table cover the three interfaces the EventPublisher
// implements, since each takes a differently typed event.
type publishFunc func(context.Context, *EventPublisher) error

type publishCase struct {
	name      string
	publish   publishFunc
	wantTopic string
	wantKey   string
	wantEvent string
}

func publishCases() []publishCase {
	return []publishCase{
		{
			name: "email confirmation requested",
			publish: func(ctx context.Context, publisher *EventPublisher) error {
				return publisher.PublishEmailConfirmationRequested(ctx, identitydomain.NewEmailConfirmationRequested(
					userID, "Wile E. Coyote", "wile.coyote@example.com", "https://tuenti.test/confirm?token=acme",
				))
			},
			wantTopic: identityEventsTopic,
			wantKey:   userID,
			wantEvent: identitydomain.EventEmailConfirmationRequested,
		},
		{
			name: "password reset requested",
			publish: func(ctx context.Context, publisher *EventPublisher) error {
				return publisher.PublishPasswordResetRequested(ctx, identitydomain.NewPasswordResetRequested(
					userID, "Wile E. Coyote", "wile.coyote@example.com", "https://tuenti.test/reset?token=acme",
				))
			},
			wantTopic: identityEventsTopic,
			wantKey:   userID,
			wantEvent: identitydomain.EventPasswordResetRequested,
		},
		{
			name: "organization invitation created",
			publish: func(ctx context.Context, publisher *EventPublisher) error {
				return publisher.PublishOrganizationInvitationCreated(ctx, orgdomain.NewOrganizationInvitationCreated(
					invitationID, "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d", "Acme Corp",
					userID, "Wile E. Coyote", "road.runner@example.com", "member",
					"https://tuenti.test/invitations/accept?token=acme",
				))
			},
			wantTopic: organizationEventsTopic,
			wantKey:   invitationID,
			wantEvent: orgdomain.EventOrganizationInvitationCreated,
		},
	}
}

func TestEventPublisherRoutesEachEventToItsTopicAndKey(t *testing.T) {
	for _, tt := range publishCases() {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeWriter{}

			require.NoError(t, tt.publish(context.Background(), NewEventPublisher(newProducer(writer))))

			require.Len(t, writer.messages, 1, "the publisher must emit exactly one message")

			message := writer.messages[0]
			assert.Equal(t, tt.wantTopic, message.Topic, "the event must be routed to its context's topic")
			assert.Equal(t, []byte(tt.wantKey), message.Key, "the partition key must keep a subject's events ordered")

			var payload map[string]any
			require.NoError(t, json.Unmarshal(message.Value, &payload))

			assert.Equal(t, tt.wantEvent, payload["event"], "the envelope event name must survive encoding")
			assert.NotEmpty(t, payload["event_id"], "the envelope must carry an event id")
			assert.NotEmpty(t, payload["occurred_at"], "the envelope must carry an occurred_at timestamp")
		})
	}
}

func TestEventPublisherPropagatesProducerFailures(t *testing.T) {
	for _, tt := range publishCases() {
		t.Run(tt.name, func(t *testing.T) {
			sentinel := errors.New("broker unavailable")
			writer := &fakeWriter{err: sentinel}

			err := tt.publish(context.Background(), NewEventPublisher(newProducer(writer)))

			require.Error(t, err, "a publish failure must reach the caller, not be swallowed here")
			assert.ErrorIs(t, err, sentinel, "the underlying client error must stay unwrappable")
			assert.Contains(t, err.Error(), tt.wantTopic, "the failing topic must be identifiable from the error")
		})
	}
}
