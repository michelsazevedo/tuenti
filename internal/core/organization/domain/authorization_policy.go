package domain

import "slices"

type AuthorizationPolicy struct{}

func (p AuthorizationPolicy) CanManageOrganization(role Role) bool {
	return role == RoleManager || role == RoleAdmin
}

func (p AuthorizationPolicy) CanManageMembers(role Role) bool {
	return role == RoleManager || role == RoleAdmin
}

func (p AuthorizationPolicy) CanManageBilling(role Role) bool {
	return role == RoleManager
}

func (p AuthorizationPolicy) CanViewOrganization(role Role) bool {
	return role == RoleManager || role == RoleAdmin || role == RoleMember
}

var roleAssignmentAllowList = map[Role][]Role{
	RoleManager: {RoleManager, RoleAdmin, RoleMember},
	RoleAdmin:   {RoleMember},
}

func (p AuthorizationPolicy) CanAssignRole(assignerRole, targetRole Role) bool {
	return slices.Contains(roleAssignmentAllowList[assignerRole], targetRole)
}

func (p AuthorizationPolicy) CanRemoveSelf(role Role, isLastManager bool) bool {
	switch role {
	case RoleManager:
		return !isLastManager
	case RoleAdmin, RoleMember:
		return true
	default:
		return false
	}
}
