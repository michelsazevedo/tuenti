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

const EventOrganizationInvitationCreated = "organization_invitation_created"

type OrganizationInvitationCreated struct {
	EventEnvelope
	InvitationID     string `json:"invitation_id"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	InviterUserID    string `json:"inviter_user_id"`
	InviterName      string `json:"inviter_name"`
	InviteeEmail     string `json:"invitee_email"`
	Role             string `json:"role"`
	InviteURL        string `json:"invite_url"`
}

func NewOrganizationInvitationCreated(
	invitationID, organizationID, organizationName,
	inviterUserID, inviterName, inviteeEmail, role, inviteURL string,
) OrganizationInvitationCreated {
	return OrganizationInvitationCreated{
		EventEnvelope:    newEventEnvelope(EventOrganizationInvitationCreated),
		InvitationID:     invitationID,
		OrganizationID:   organizationID,
		OrganizationName: organizationName,
		InviterUserID:    inviterUserID,
		InviterName:      inviterName,
		InviteeEmail:     inviteeEmail,
		Role:             role,
		InviteURL:        inviteURL,
	}
}
