package api

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const (
	invitationStatusPending  = "pending"
	invitationStatusAccepted = "accepted"
	invitationStatusRevoked  = "revoked"
	invitationStatusExpired  = "expired"
)

type InvitationResponse struct {
	Id                    pgtype.UUID `json:"id"`
	Email                 string      `json:"email"`
	Role                  domain.Role `json:"role"`
	Status                string      `json:"status"`
	CreatedAt             time.Time   `json:"created_at"`
	ExpiresAt             time.Time   `json:"expires_at"`
	AcceptedAt            *time.Time  `json:"accepted_at"`
	RevokedAt             *time.Time  `json:"revoked_at"`
	InvitedByMembershipId pgtype.UUID `json:"invited_by_membership_id"`
}

func NewInvitationResponse(invitation *domain.Invitation) InvitationResponse {
	return InvitationResponse{
		Id:                    invitation.Id,
		Email:                 invitation.Email,
		Role:                  invitation.Role,
		Status:                invitationStatus(invitation),
		CreatedAt:             invitation.CreatedAt,
		ExpiresAt:             invitation.ExpiresAt,
		AcceptedAt:            invitation.AcceptedAt,
		RevokedAt:             invitation.RevokedAt,
		InvitedByMembershipId: invitation.InvitedByMembershipId,
	}
}

func NewInvitationResponses(invitations []*domain.Invitation) []InvitationResponse {
	responses := make([]InvitationResponse, 0, len(invitations))

	for _, invitation := range invitations {
		responses = append(responses, NewInvitationResponse(invitation))
	}

	return responses
}

func invitationStatus(invitation *domain.Invitation) string {
	switch {
	case invitation.IsRevoked():
		return invitationStatusRevoked
	case invitation.IsAccepted():
		return invitationStatusAccepted
	case invitation.IsExpired(time.Now().UTC()):
		return invitationStatusExpired
	default:
		return invitationStatusPending
	}
}

type MembershipResponse struct {
	Id             pgtype.UUID `json:"id"`
	OrganizationId pgtype.UUID `json:"organization_id"`
	UserId         pgtype.UUID `json:"user_id"`
	Role           domain.Role `json:"role"`
}

func NewMembershipResponse(membership *domain.Membership) MembershipResponse {
	return MembershipResponse{
		Id:             membership.Id,
		OrganizationId: membership.OrganizationId,
		UserId:         membership.UserId,
		Role:           membership.Role,
	}
}
