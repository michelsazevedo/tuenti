package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	invitationID     = "0b2a4c5e-1d3f-4a6b-8c9d-0e1f2a3b4c5d"
	organizationID   = "1c3b5d6f-2e4a-4b7c-9d0e-1f2a3b4c5d6e"
	organizationName = "Acme"
	inviterUserID    = "2d4c6e7a-3f5b-4c8d-8e1f-2a3b4c5d6e7f"
	inviterName      = "Michel"
	inviteeEmail     = "user@example.com"
	invitationRole   = "member"
	inviteURL        = "https://app.example.com/invitations/accept?token=abc"
)

func newTestInvitationCreated() OrganizationInvitationCreated {
	return NewOrganizationInvitationCreated(
		invitationID,
		organizationID,
		organizationName,
		inviterUserID,
		inviterName,
		inviteeEmail,
		invitationRole,
		inviteURL,
	)
}

func TestNewOrganizationInvitationCreated(t *testing.T) {
	t.Parallel()

	t.Run("stamps the envelope", func(t *testing.T) {
		t.Parallel()

		before := time.Now().UTC()
		event := newTestInvitationCreated()
		after := time.Now().UTC()

		assert.Equal(t, EventOrganizationInvitationCreated, event.Event)
		assert.Equal(t, "organization_invitation_created", event.Event)

		require.NotEmpty(t, event.EventID)
		_, err := uuid.Parse(event.EventID)
		assert.NoError(t, err, "event_id must be a valid UUID")

		assert.False(t, event.OccurredAt.Before(before), "occurred_at must not precede construction")
		assert.False(t, event.OccurredAt.After(after), "occurred_at must not follow construction")
		assert.Equal(t, time.UTC, event.OccurredAt.Location(), "occurred_at must be UTC")
	})

	t.Run("carries the payload it was given", func(t *testing.T) {
		t.Parallel()

		event := newTestInvitationCreated()

		assert.Equal(t, invitationID, event.InvitationID)
		assert.Equal(t, organizationID, event.OrganizationID)
		assert.Equal(t, organizationName, event.OrganizationName)
		assert.Equal(t, inviterUserID, event.InviterUserID)
		assert.Equal(t, inviterName, event.InviterName)
		assert.Equal(t, inviteeEmail, event.InviteeEmail)
		assert.Equal(t, invitationRole, event.Role)
		assert.Equal(t, inviteURL, event.InviteURL)
	})

	t.Run("issues a distinct event id per event", func(t *testing.T) {
		t.Parallel()

		first := newTestInvitationCreated()
		second := newTestInvitationCreated()

		assert.NotEqual(t, first.EventID, second.EventID)
	})
}

func TestOrganizationInvitationCreatedMarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("flattens the envelope into the payload", func(t *testing.T) {
		t.Parallel()

		event := newTestInvitationCreated()

		raw, err := json.Marshal(event)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))

		assert.Equal(t, EventOrganizationInvitationCreated, payload["event"])
		assert.Equal(t, event.EventID, payload["event_id"])
		assert.Equal(t, event.OccurredAt.Format(time.RFC3339Nano), payload["occurred_at"])
		assert.Equal(t, invitationID, payload["invitation_id"])
		assert.Equal(t, organizationID, payload["organization_id"])
		assert.Equal(t, organizationName, payload["organization_name"])
		assert.Equal(t, inviterUserID, payload["inviter_user_id"])
		assert.Equal(t, inviterName, payload["inviter_name"])
		assert.Equal(t, inviteeEmail, payload["invitee_email"])
		assert.Equal(t, invitationRole, payload["role"])
		assert.Equal(t, inviteURL, payload["invite_url"])
	})

	t.Run("emits exactly the contracted keys", func(t *testing.T) {
		t.Parallel()

		raw, err := json.Marshal(newTestInvitationCreated())
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))

		expected := []string{
			"event",
			"event_id",
			"occurred_at",
			"invitation_id",
			"organization_id",
			"organization_name",
			"inviter_user_id",
			"inviter_name",
			"invitee_email",
			"role",
			"invite_url",
		}

		keys := make([]string, 0, len(payload))
		for key := range payload {
			keys = append(keys, key)
		}

		assert.ElementsMatch(t, expected, keys, "event payload keys are a published contract")
	})

	t.Run("serialises occurred_at as an RFC 3339 UTC timestamp", func(t *testing.T) {
		t.Parallel()

		event := newTestInvitationCreated()
		event.OccurredAt = time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)

		raw, err := json.Marshal(event)
		require.NoError(t, err)

		var payload struct {
			OccurredAt string `json:"occurred_at"`
		}
		require.NoError(t, json.Unmarshal(raw, &payload))

		assert.Equal(t, "2026-08-15T10:00:00Z", payload.OccurredAt)
	})

	t.Run("round-trips back into the same event", func(t *testing.T) {
		t.Parallel()

		event := newTestInvitationCreated()

		raw, err := json.Marshal(event)
		require.NoError(t, err)

		var decoded OrganizationInvitationCreated
		require.NoError(t, json.Unmarshal(raw, &decoded))

		assert.Equal(t, event, decoded)
	})
}
