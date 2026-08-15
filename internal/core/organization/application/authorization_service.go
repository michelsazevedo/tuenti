package application

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type MembershipAuthorizationService interface {
	Authorize(ctx context.Context, userID, organizationID pgtype.UUID) (*MembershipAuthorization, error)
}

type membershipAuthorizationService struct {
	memberships domain.MembershipRepository
	policy      domain.AuthorizationPolicy
}

func NewMembershipAuthorizationService(memberships domain.MembershipRepository) MembershipAuthorizationService {
	return &membershipAuthorizationService{memberships: memberships, policy: domain.AuthorizationPolicy{}}
}

func (s *membershipAuthorizationService) Authorize(
	ctx context.Context, userID, organizationID pgtype.UUID,
) (*MembershipAuthorization, error) {
	membership, err := s.memberships.FindByUserAndOrganization(ctx, userID, organizationID)
	if err != nil {
		return nil, err
	}

	return &MembershipAuthorization{membership: membership, policy: s.policy}, nil
}

type MembershipAuthorization struct {
	membership *domain.Membership
	policy     domain.AuthorizationPolicy
}

func (a *MembershipAuthorization) Membership() *domain.Membership {
	return a.membership
}

func (a *MembershipAuthorization) CanManageOrganization() bool {
	return a.policy.CanManageOrganization(a.membership.Role)
}

func (a *MembershipAuthorization) CanManageMembers() bool {
	return a.policy.CanManageMembers(a.membership.Role)
}

func (a *MembershipAuthorization) CanManageBilling() bool {
	return a.policy.CanManageBilling(a.membership.Role)
}

func (a *MembershipAuthorization) CanViewOrganization() bool {
	return a.policy.CanViewOrganization(a.membership.Role)
}

func (a *MembershipAuthorization) CanAssignRole(targetRole domain.Role) bool {
	return a.policy.CanAssignRole(a.membership.Role, targetRole)
}

func (a *MembershipAuthorization) CanRemoveSelf(isLastManager bool) bool {
	return a.policy.CanRemoveSelf(a.membership.Role, isLastManager)
}
