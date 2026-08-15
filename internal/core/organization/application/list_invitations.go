package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type ListInvitationsUseCase interface {
	ListInvitations(ctx context.Context, organizationID pgtype.UUID) ([]*domain.Invitation, error)
}

type listInvitations struct {
	invitations domain.InvitationRepository
}

func NewListInvitations(invitations domain.InvitationRepository) ListInvitationsUseCase {
	return &listInvitations{invitations: invitations}
}

func (l *listInvitations) ListInvitations(ctx context.Context, organizationID pgtype.UUID) ([]*domain.Invitation, error) {
	return l.invitations.FindByOrganization(ctx, organizationID)
}
