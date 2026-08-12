package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoleValid(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{name: "owner", role: RoleOwner, want: true},
		{name: "admin", role: RoleAdmin, want: true},
		{name: "member", role: RoleMember, want: true},
		{name: "unknown role", role: Role("superadmin"), want: false},
		{name: "empty role", role: Role(""), want: false},
		{name: "wrong case", role: Role("Owner"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.role.Valid())
		})
	}
}
