package domain

type Role string

const (
	RoleManager Role = "manager"
	RoleAdmin   Role = "admin"
	RoleMember  Role = "member"
)

func (r Role) Valid() bool {
	switch r {
	case RoleManager, RoleAdmin, RoleMember:
		return true
	default:
		return false
	}
}
