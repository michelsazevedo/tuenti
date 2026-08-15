package domain

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type Invitation struct {
	Id                    pgtype.UUID `json:"id"`
	OrganizationId        pgtype.UUID `json:"organization_id"`
	Email                 string      `json:"email" validate:"required,email"`
	Role                  Role        `json:"role" validate:"required,oneof=manager admin member"`
	TokenDigest           string      `json:"-"`
	InvitedByMembershipId pgtype.UUID `json:"invited_by_membership_id"`
	CreatedAt             time.Time   `json:"created_at"`
	ExpiresAt             time.Time   `json:"expires_at"`
	AcceptedAt            *time.Time  `json:"accepted_at"`
	RevokedAt             *time.Time  `json:"revoked_at"`
}

func (i *Invitation) IsExpired(now time.Time) bool {
	return !now.Before(i.ExpiresAt)
}

func (i *Invitation) IsRevoked() bool {
	return i.RevokedAt != nil
}

func (i *Invitation) IsAccepted() bool {
	return i.AcceptedAt != nil
}

func (i *Invitation) Validate(now time.Time) error {
	if i.IsAccepted() {
		return ErrInvitationAlreadyAccepted
	}

	if i.IsRevoked() {
		return ErrInvitationRevoked
	}

	if i.IsExpired(now) {
		return ErrInvitationExpired
	}

	return nil
}
