package domain

import "context"

type InvitationMailer interface {
	SendInvitation(ctx context.Context, email, organizationName, role, invitationURL string) error
}
