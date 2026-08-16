package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

func TestNewEmailConfirmationRequested(t *testing.T) {
	t.Parallel()

	t.Run("carries the event name", func(t *testing.T) {
		t.Parallel()

		event := domain.NewEmailConfirmationRequested("user-id", "Michel", "user@example.com", "https://app.test/confirm")

		assert.Equal(t, domain.EventEmailConfirmationRequested, event.Event)
		assert.Equal(t, "email_confirmation_requested", event.Event)
	})

	t.Run("carries the payload it was built with", func(t *testing.T) {
		t.Parallel()

		event := domain.NewEmailConfirmationRequested("user-id", "Michel", "user@example.com", "https://app.test/confirm")

		assert.Equal(t, "user-id", event.UserID)
		assert.Equal(t, "Michel", event.Name)
		assert.Equal(t, "user@example.com", event.Email)
		assert.Equal(t, "https://app.test/confirm", event.ConfirmationURL)
	})

	t.Run("stamps a valid version 4 uuid as the event id", func(t *testing.T) {
		t.Parallel()

		event := domain.NewEmailConfirmationRequested("user-id", "Michel", "user@example.com", "https://app.test/confirm")

		require.NotEmpty(t, event.EventID)

		id, err := uuid.Parse(event.EventID)
		require.NoError(t, err, "event id must be a valid uuid")
		assert.Equal(t, uuid.Version(4), id.Version())
	})

	t.Run("does not reuse event ids across calls", func(t *testing.T) {
		t.Parallel()

		const samples = 1_000

		seen := make(map[string]struct{}, samples)
		for range samples {
			event := domain.NewEmailConfirmationRequested("user-id", "Michel", "user@example.com", "https://app.test/confirm")

			_, duplicate := seen[event.EventID]
			require.False(t, duplicate, "generated a duplicate event id")

			seen[event.EventID] = struct{}{}
		}

		assert.Len(t, seen, samples)
	})

	t.Run("stamps occurred at in utc, close to now", func(t *testing.T) {
		t.Parallel()

		before := time.Now().UTC()
		event := domain.NewEmailConfirmationRequested("user-id", "Michel", "user@example.com", "https://app.test/confirm")
		after := time.Now().UTC()

		assert.Equal(t, time.UTC, event.OccurredAt.Location(), "occurred at must be utc, so consumers never guess the offset")
		assert.False(t, event.OccurredAt.Before(before), "occurred at must not predate the call")
		assert.False(t, event.OccurredAt.After(after), "occurred at must not postdate the call")
	})
}

func TestNewPasswordResetRequested(t *testing.T) {
	t.Parallel()

	t.Run("carries the event name", func(t *testing.T) {
		t.Parallel()

		event := domain.NewPasswordResetRequested("user-id", "Michel", "user@example.com", "https://app.test/reset")

		assert.Equal(t, domain.EventPasswordResetRequested, event.Event)
		assert.Equal(t, "password_reset_requested", event.Event)
	})

	t.Run("carries the payload it was built with", func(t *testing.T) {
		t.Parallel()

		event := domain.NewPasswordResetRequested("user-id", "Michel", "user@example.com", "https://app.test/reset")

		assert.Equal(t, "user-id", event.UserID)
		assert.Equal(t, "Michel", event.Name)
		assert.Equal(t, "user@example.com", event.Email)
		assert.Equal(t, "https://app.test/reset", event.ResetURL)
	})

	t.Run("stamps a valid version 4 uuid as the event id", func(t *testing.T) {
		t.Parallel()

		event := domain.NewPasswordResetRequested("user-id", "Michel", "user@example.com", "https://app.test/reset")

		require.NotEmpty(t, event.EventID)

		id, err := uuid.Parse(event.EventID)
		require.NoError(t, err, "event id must be a valid uuid")
		assert.Equal(t, uuid.Version(4), id.Version())
	})

	t.Run("does not reuse event ids across calls", func(t *testing.T) {
		t.Parallel()

		const samples = 1_000

		seen := make(map[string]struct{}, samples)
		for range samples {
			event := domain.NewPasswordResetRequested("user-id", "Michel", "user@example.com", "https://app.test/reset")

			_, duplicate := seen[event.EventID]
			require.False(t, duplicate, "generated a duplicate event id")

			seen[event.EventID] = struct{}{}
		}

		assert.Len(t, seen, samples)
	})

	t.Run("stamps occurred at in utc, close to now", func(t *testing.T) {
		t.Parallel()

		before := time.Now().UTC()
		event := domain.NewPasswordResetRequested("user-id", "Michel", "user@example.com", "https://app.test/reset")
		after := time.Now().UTC()

		assert.Equal(t, time.UTC, event.OccurredAt.Location(), "occurred at must be utc, so consumers never guess the offset")
		assert.False(t, event.OccurredAt.Before(before), "occurred at must not predate the call")
		assert.False(t, event.OccurredAt.After(after), "occurred at must not postdate the call")
	})
}

func TestEmailConfirmationRequestedMarshalJSON(t *testing.T) {
	t.Parallel()

	event := domain.NewEmailConfirmationRequested(
		"3f2b1c4d-5e6f-4a8b-9c0d-1e2f3a4b5c6d",
		"Michel",
		"user@example.com",
		"https://app.test/confirm?token=abc",
	)

	payload := marshalToMap(t, event)

	t.Run("flattens the envelope alongside the payload", func(t *testing.T) {
		assert.ElementsMatch(
			t,
			[]string{"event", "event_id", "occurred_at", "user_id", "name", "email", "confirmation_url"},
			keysOf(payload),
			"the wire contract is consumed by the notification service, so extra or missing keys are breaking changes",
		)
	})

	t.Run("carries the expected values", func(t *testing.T) {
		assert.Equal(t, "email_confirmation_requested", payload["event"])
		assert.Equal(t, "3f2b1c4d-5e6f-4a8b-9c0d-1e2f3a4b5c6d", payload["user_id"])
		assert.Equal(t, "Michel", payload["name"])
		assert.Equal(t, "user@example.com", payload["email"])
		assert.Equal(t, "https://app.test/confirm?token=abc", payload["confirmation_url"])
	})

	t.Run("encodes the envelope as strings a consumer can parse", func(t *testing.T) {
		eventID, ok := payload["event_id"].(string)
		require.True(t, ok, "event_id must marshal as a string")
		_, err := uuid.Parse(eventID)
		assert.NoError(t, err)

		occurredAt, ok := payload["occurred_at"].(string)
		require.True(t, ok, "occurred_at must marshal as a string")
		parsed, err := time.Parse(time.RFC3339Nano, occurredAt)
		require.NoError(t, err, "occurred_at must be rfc3339")
		assert.Equal(t, event.OccurredAt.UTC(), parsed.UTC())
	})
}

func TestPasswordResetRequestedMarshalJSON(t *testing.T) {
	t.Parallel()

	event := domain.NewPasswordResetRequested(
		"3f2b1c4d-5e6f-4a8b-9c0d-1e2f3a4b5c6d",
		"Michel",
		"user@example.com",
		"https://app.test/reset?token=abc",
	)

	payload := marshalToMap(t, event)

	t.Run("flattens the envelope alongside the payload", func(t *testing.T) {
		assert.ElementsMatch(
			t,
			[]string{"event", "event_id", "occurred_at", "user_id", "name", "email", "reset_url"},
			keysOf(payload),
			"the wire contract is consumed by the notification service, so extra or missing keys are breaking changes",
		)
	})

	t.Run("carries the expected values", func(t *testing.T) {
		assert.Equal(t, "password_reset_requested", payload["event"])
		assert.Equal(t, "3f2b1c4d-5e6f-4a8b-9c0d-1e2f3a4b5c6d", payload["user_id"])
		assert.Equal(t, "Michel", payload["name"])
		assert.Equal(t, "user@example.com", payload["email"])
		assert.Equal(t, "https://app.test/reset?token=abc", payload["reset_url"])
	})

	t.Run("encodes the envelope as strings a consumer can parse", func(t *testing.T) {
		eventID, ok := payload["event_id"].(string)
		require.True(t, ok, "event_id must marshal as a string")
		_, err := uuid.Parse(eventID)
		assert.NoError(t, err)

		occurredAt, ok := payload["occurred_at"].(string)
		require.True(t, ok, "occurred_at must marshal as a string")
		parsed, err := time.Parse(time.RFC3339Nano, occurredAt)
		require.NoError(t, err, "occurred_at must be rfc3339")
		assert.Equal(t, event.OccurredAt.UTC(), parsed.UTC())
	})
}

func marshalToMap(t *testing.T, event any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(event)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))

	return payload
}

func keysOf(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}

	return keys
}
