package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthorizationPolicy(t *testing.T) {
	tests := []struct {
		name                      string
		role                      Role
		wantCanManageOrganization bool
		wantCanManageMembers      bool
		wantCanManageBilling      bool
		wantCanViewOrganization   bool
	}{
		{
			name:                      "manager can do everything",
			role:                      RoleManager,
			wantCanManageOrganization: true,
			wantCanManageMembers:      true,
			wantCanManageBilling:      true,
			wantCanViewOrganization:   true,
		},
		{
			name:                      "admin can do everything except manage billing",
			role:                      RoleAdmin,
			wantCanManageOrganization: true,
			wantCanManageMembers:      true,
			wantCanManageBilling:      false,
			wantCanViewOrganization:   true,
		},
		{
			name:                      "member can only view",
			role:                      RoleMember,
			wantCanManageOrganization: false,
			wantCanManageMembers:      false,
			wantCanManageBilling:      false,
			wantCanViewOrganization:   true,
		},
	}

	policy := AuthorizationPolicy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantCanManageOrganization, policy.CanManageOrganization(tt.role), "CanManageOrganization")
			assert.Equal(t, tt.wantCanManageMembers, policy.CanManageMembers(tt.role), "CanManageMembers")
			assert.Equal(t, tt.wantCanManageBilling, policy.CanManageBilling(tt.role), "CanManageBilling")
			assert.Equal(t, tt.wantCanViewOrganization, policy.CanViewOrganization(tt.role), "CanViewOrganization")
		})
	}
}

func TestAuthorizationPolicyOnlyManagerCanManageBilling(t *testing.T) {
	policy := AuthorizationPolicy{}

	assert.True(t, policy.CanManageBilling(RoleManager), "manager must be able to manage billing")
	assert.False(t, policy.CanManageBilling(RoleAdmin), "admin must not be able to manage billing")
	assert.False(t, policy.CanManageBilling(RoleMember), "member must not be able to manage billing")
}

func TestAuthorizationPolicyDeniesUnknownRole(t *testing.T) {
	policy := AuthorizationPolicy{}

	unknownRoles := []Role{Role(""), Role("owner"), Role("superuser")}

	for _, role := range unknownRoles {
		t.Run(string(role), func(t *testing.T) {
			assert.False(t, policy.CanManageOrganization(role), "CanManageOrganization")
			assert.False(t, policy.CanManageMembers(role), "CanManageMembers")
			assert.False(t, policy.CanManageBilling(role), "CanManageBilling")
			assert.False(t, policy.CanViewOrganization(role), "CanViewOrganization")
			assert.False(t, policy.CanAssignRole(role, RoleMember), "CanAssignRole as assigner")
			assert.False(t, policy.CanAssignRole(RoleManager, role), "CanAssignRole as target")
			assert.False(t, policy.CanRemoveSelf(role, false), "CanRemoveSelf, not last manager")
			assert.False(t, policy.CanRemoveSelf(role, true), "CanRemoveSelf, last manager")
			assert.False(t, policy.CanCreateInvitation(role), "CanCreateInvitation")
			assert.False(t, policy.CanRevokeInvitation(role, RoleMember), "CanRevokeInvitation as revoker")
			assert.False(t, policy.CanRevokeInvitation(RoleManager, role), "CanRevokeInvitation as target")
		})
	}
}

func TestAuthorizationPolicyCanCreateInvitation(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{name: "manager may invite", role: RoleManager, want: true},
		{name: "admin may invite", role: RoleAdmin, want: true},
		{name: "member may not invite", role: RoleMember, want: false},
	}

	policy := AuthorizationPolicy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policy.CanCreateInvitation(tt.role))
		})
	}
}

func TestAuthorizationPolicyCanRevokeInvitation(t *testing.T) {
	tests := []struct {
		name        string
		revokerRole Role
		targetRole  Role
		want        bool
	}{
		{
			name:        "manager may revoke a manager invitation",
			revokerRole: RoleManager,
			targetRole:  RoleManager,
			want:        true,
		},
		{
			name:        "manager may revoke an admin invitation",
			revokerRole: RoleManager,
			targetRole:  RoleAdmin,
			want:        true,
		},
		{
			name:        "manager may revoke a member invitation",
			revokerRole: RoleManager,
			targetRole:  RoleMember,
			want:        true,
		},
		{
			name:        "admin may not revoke a manager invitation",
			revokerRole: RoleAdmin,
			targetRole:  RoleManager,
			want:        false,
		},
		{
			name:        "admin may revoke an admin invitation",
			revokerRole: RoleAdmin,
			targetRole:  RoleAdmin,
			want:        true,
		},
		{
			name:        "admin may revoke a member invitation",
			revokerRole: RoleAdmin,
			targetRole:  RoleMember,
			want:        true,
		},
		{
			name:        "member may not revoke a manager invitation",
			revokerRole: RoleMember,
			targetRole:  RoleManager,
			want:        false,
		},
		{
			name:        "member may not revoke an admin invitation",
			revokerRole: RoleMember,
			targetRole:  RoleAdmin,
			want:        false,
		},
		{
			name:        "member may not revoke a member invitation",
			revokerRole: RoleMember,
			targetRole:  RoleMember,
			want:        false,
		},
	}

	policy := AuthorizationPolicy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policy.CanRevokeInvitation(tt.revokerRole, tt.targetRole))
		})
	}
}

func TestAuthorizationPolicyCanAssignRole(t *testing.T) {
	tests := []struct {
		name         string
		assignerRole Role
		targetRole   Role
		want         bool
	}{
		{
			name:         "filled gap: manager may promote another member to manager",
			assignerRole: RoleManager,
			targetRole:   RoleManager,
			want:         true,
		},
		{
			name:         "manager may assign admin",
			assignerRole: RoleManager,
			targetRole:   RoleAdmin,
			want:         true,
		},
		{
			name:         "manager may assign member",
			assignerRole: RoleManager,
			targetRole:   RoleMember,
			want:         true,
		},
		{
			name:         "admin may not assign manager",
			assignerRole: RoleAdmin,
			targetRole:   RoleManager,
			want:         false,
		},
		{
			name:         "filled gap: admin may not assign admin",
			assignerRole: RoleAdmin,
			targetRole:   RoleAdmin,
			want:         false,
		},
		{
			name:         "admin may assign member",
			assignerRole: RoleAdmin,
			targetRole:   RoleMember,
			want:         true,
		},
		{
			name:         "member may not assign manager",
			assignerRole: RoleMember,
			targetRole:   RoleManager,
			want:         false,
		},
		{
			name:         "member may not assign admin",
			assignerRole: RoleMember,
			targetRole:   RoleAdmin,
			want:         false,
		},
		{
			name:         "member may not assign member",
			assignerRole: RoleMember,
			targetRole:   RoleMember,
			want:         false,
		},
	}

	policy := AuthorizationPolicy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policy.CanAssignRole(tt.assignerRole, tt.targetRole))
		})
	}
}

func TestAuthorizationPolicyCanAssignInvitationRole(t *testing.T) {
	tests := []struct {
		name        string
		inviterRole Role
		targetRole  Role
		want        bool
	}{
		{name: "manager may invite a manager", inviterRole: RoleManager, targetRole: RoleManager, want: true},
		{name: "manager may invite an admin", inviterRole: RoleManager, targetRole: RoleAdmin, want: true},
		{name: "manager may invite a member", inviterRole: RoleManager, targetRole: RoleMember, want: true},
		{name: "admin may not invite a manager", inviterRole: RoleAdmin, targetRole: RoleManager, want: false},
		{
			name:        "admin may invite an admin (unlike CanAssignRole, which forbids admin-to-admin promotion)",
			inviterRole: RoleAdmin, targetRole: RoleAdmin, want: true,
		},
		{name: "admin may invite a member", inviterRole: RoleAdmin, targetRole: RoleMember, want: true},
		{name: "member may not invite a manager", inviterRole: RoleMember, targetRole: RoleManager, want: false},
		{name: "member may not invite an admin", inviterRole: RoleMember, targetRole: RoleAdmin, want: false},
		{name: "member may not invite a member", inviterRole: RoleMember, targetRole: RoleMember, want: false},
	}

	policy := AuthorizationPolicy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policy.CanAssignInvitationRole(tt.inviterRole, tt.targetRole))
		})
	}
}

func TestAuthorizationPolicyCanRemoveSelf(t *testing.T) {
	tests := []struct {
		name          string
		role          Role
		isLastManager bool
		want          bool
	}{
		{
			name:          "the last manager may not leave the organization",
			role:          RoleManager,
			isLastManager: true,
			want:          false,
		},
		{
			name:          "a manager who is not the last one may leave",
			role:          RoleManager,
			isLastManager: false,
			want:          true,
		},
		{
			name:          "an admin may leave even if the flag is set, which is meaningless for a non manager",
			role:          RoleAdmin,
			isLastManager: true,
			want:          true,
		},
		{
			name:          "an admin may leave",
			role:          RoleAdmin,
			isLastManager: false,
			want:          true,
		},
		{
			name:          "a member may leave even if the flag is set, which is meaningless for a non manager",
			role:          RoleMember,
			isLastManager: true,
			want:          true,
		},
		{
			name:          "a member may leave",
			role:          RoleMember,
			isLastManager: false,
			want:          true,
		},
	}

	policy := AuthorizationPolicy{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policy.CanRemoveSelf(tt.role, tt.isLastManager))
		})
	}
}
