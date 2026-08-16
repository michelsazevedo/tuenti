package domain

import "context"

type InvitationEventPublisher interface {
	PublishOrganizationInvitationCreated(ctx context.Context, event OrganizationInvitationCreated) error
}
