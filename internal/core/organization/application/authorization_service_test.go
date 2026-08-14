package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type authorizeSpyMembershipRepository struct {
	membership *domain.Membership
	findErr    error
	findCalls  int
	findUserID pgtype.UUID
	findOrgID  pgtype.UUID
}

func (r *authorizeSpyMembershipRepository) Create(ctx context.Context, membership *domain.Membership) error {
	return errors.New("unexpected call to Create")
}

func (r *authorizeSpyMembershipRepository) FindByUserID(ctx context.Context, userID pgtype.UUID) (*domain.Membership, error) {
	return nil, errors.New("unexpected call to FindByUserID")
}

func (r *authorizeSpyMembershipRepository) FindByUserAndOrganization(ctx context.Context, userID, organizationID pgtype.UUID) (*domain.Membership, error) {
	r.findCalls++
	r.findUserID = userID
	r.findOrgID = organizationID

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.membership, nil
}

func userID(seed byte) pgtype.UUID {
	id := pgtype.UUID{Valid: true}
	for i := range id.Bytes {
		id.Bytes[i] = seed
	}

	return id
}

func membershipWithRole(role domain.Role) *domain.Membership {
	return &domain.Membership{
		Id:             organizationID(0x30),
		OrganizationId: organizationID(0x20),
		UserId:         userID(0x10),
		Role:           role,
		CreatedAt:      time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC),
	}
}

func authorize(t *testing.T, membership *domain.Membership) *MembershipAuthorization {
	t.Helper()

	memberships := &authorizeSpyMembershipRepository{membership: membership}

	authorization, err := NewMembershipAuthorizationService(memberships).
		Authorize(context.Background(), membership.UserId, membership.OrganizationId)
	require.NoError(t, err)
	require.NotNil(t, authorization)

	return authorization
}

func TestAuthorizeResolvesTheMembership(t *testing.T) {
	membership := membershipWithRole(domain.RoleAdmin)
	memberships := &authorizeSpyMembershipRepository{membership: membership}

	authorization, err := NewMembershipAuthorizationService(memberships).
		Authorize(context.Background(), membership.UserId, membership.OrganizationId)

	assert.NoError(t, err)
	require.NotNil(t, authorization)
	assert.Same(t, membership, authorization.Membership())

	assert.Equal(t, 1, memberships.findCalls)
	assert.Equal(t, membership.UserId, memberships.findUserID)
	assert.Equal(t, membership.OrganizationId, memberships.findOrgID)
}

func TestAuthorizeWhenTheUserHasNoMembershipInTheOrganization(t *testing.T) {
	memberships := &authorizeSpyMembershipRepository{findErr: domain.ErrMembershipNotFound}

	authorization, err := NewMembershipAuthorizationService(memberships).
		Authorize(context.Background(), userID(0x10), organizationID(0x20))

	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	assert.Nil(t, authorization)
	assert.Equal(t, 1, memberships.findCalls)
}

func TestAuthorizeWhenTheLookupFails(t *testing.T) {
	infraErr := errors.New("postgres: connection refused")
	memberships := &authorizeSpyMembershipRepository{findErr: infraErr}

	authorization, err := NewMembershipAuthorizationService(memberships).
		Authorize(context.Background(), userID(0x10), organizationID(0x20))

	assert.ErrorIs(t, err, infraErr)
	assert.Nil(t, authorization)
}

func TestMembershipAuthorizationDelegatesToThePolicy(t *testing.T) {
	policy := domain.AuthorizationPolicy{}

	tests := []struct {
		name                   string
		role                   domain.Role
		wantManageOrganization bool
		wantManageMembers      bool
		wantManageBilling      bool
		wantViewOrganization   bool
	}{
		{
			name:                   "manager",
			role:                   domain.RoleManager,
			wantManageOrganization: true,
			wantManageMembers:      true,
			wantManageBilling:      true,
			wantViewOrganization:   true,
		},
		{
			name:                   "admin",
			role:                   domain.RoleAdmin,
			wantManageOrganization: true,
			wantManageMembers:      true,
			wantManageBilling:      false,
			wantViewOrganization:   true,
		},
		{
			name:                   "member",
			role:                   domain.RoleMember,
			wantManageOrganization: false,
			wantManageMembers:      false,
			wantManageBilling:      false,
			wantViewOrganization:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorization := authorize(t, membershipWithRole(tt.role))

			assert.Equal(t, tt.wantManageOrganization, authorization.CanManageOrganization())
			assert.Equal(t, tt.wantManageMembers, authorization.CanManageMembers())
			assert.Equal(t, tt.wantManageBilling, authorization.CanManageBilling())
			assert.Equal(t, tt.wantViewOrganization, authorization.CanViewOrganization())

			assert.Equal(t, policy.CanManageOrganization(tt.role), authorization.CanManageOrganization())
			assert.Equal(t, policy.CanManageMembers(tt.role), authorization.CanManageMembers())
			assert.Equal(t, policy.CanManageBilling(tt.role), authorization.CanManageBilling())
			assert.Equal(t, policy.CanViewOrganization(tt.role), authorization.CanViewOrganization())
		})
	}
}

func TestMembershipAuthorizationCanAssignRole(t *testing.T) {
	policy := domain.AuthorizationPolicy{}

	tests := []struct {
		name       string
		role       domain.Role
		targetRole domain.Role
		want       bool
	}{
		{name: "manager assigns member", role: domain.RoleManager, targetRole: domain.RoleMember, want: true},
		{name: "member assigns member", role: domain.RoleMember, targetRole: domain.RoleMember, want: false},
		{name: "admin assigns manager", role: domain.RoleAdmin, targetRole: domain.RoleManager, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorization := authorize(t, membershipWithRole(tt.role))

			assert.Equal(t, tt.want, authorization.CanAssignRole(tt.targetRole))
			assert.Equal(t, policy.CanAssignRole(tt.role, tt.targetRole), authorization.CanAssignRole(tt.targetRole))
		})
	}
}

func TestMembershipAuthorizationCanRemoveSelf(t *testing.T) {
	policy := domain.AuthorizationPolicy{}

	tests := []struct {
		name          string
		role          domain.Role
		isLastManager bool
		want          bool
	}{
		{name: "last manager may not leave", role: domain.RoleManager, isLastManager: true, want: false},
		{name: "manager alongside another manager may leave", role: domain.RoleManager, isLastManager: false, want: true},
		{name: "member may always leave", role: domain.RoleMember, isLastManager: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorization := authorize(t, membershipWithRole(tt.role))

			assert.Equal(t, tt.want, authorization.CanRemoveSelf(tt.isLastManager))
			assert.Equal(t, policy.CanRemoveSelf(tt.role, tt.isLastManager), authorization.CanRemoveSelf(tt.isLastManager))
		})
	}
}
