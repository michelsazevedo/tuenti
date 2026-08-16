package domain

import (
	"time"

	"github.com/google/uuid"
)

type EventEnvelope struct {
	Event      string    `json:"event"`
	EventID    string    `json:"event_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

func newEventEnvelope(event string) EventEnvelope {
	return EventEnvelope{
		Event:      event,
		EventID:    uuid.NewString(),
		OccurredAt: time.Now().UTC(),
	}
}

const (
	EventEmailConfirmationRequested = "email_confirmation_requested"
	EventPasswordResetRequested     = "password_reset_requested"
)

type EmailConfirmationRequested struct {
	EventEnvelope
	UserID          string `json:"user_id"`
	Name            string `json:"name"`
	Email           string `json:"email"`
	ConfirmationURL string `json:"confirmation_url"`
}

func NewEmailConfirmationRequested(userID, name, email, confirmationURL string) EmailConfirmationRequested {
	return EmailConfirmationRequested{
		EventEnvelope:   newEventEnvelope(EventEmailConfirmationRequested),
		UserID:          userID,
		Name:            name,
		Email:           email,
		ConfirmationURL: confirmationURL,
	}
}

type PasswordResetRequested struct {
	EventEnvelope
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	ResetURL string `json:"reset_url"`
}

func NewPasswordResetRequested(userID, name, email, resetURL string) PasswordResetRequested {
	return PasswordResetRequested{
		EventEnvelope: newEventEnvelope(EventPasswordResetRequested),
		UserID:        userID,
		Name:          name,
		Email:         email,
		ResetURL:      resetURL,
	}
}
