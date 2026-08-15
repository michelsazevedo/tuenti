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
