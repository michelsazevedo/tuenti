package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const (
	permissionUserID = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

	permissionOrganizationID = "6ba7b811-9dad-11d1-80b4-00c04fd430c8"
)

type permissionMembershipRepository struct {
	membership *orgdomain.Membership
	findErr    error

	findCalls          int
	findUserID         pgtype.UUID
	findOrganizationID pgtype.UUID
}

func (r *permissionMembershipRepository) Create(context.Context, *orgdomain.Membership) error {
	return errors.New("unexpected call to Create")
}

func (r *permissionMembershipRepository) FindByUserID(context.Context, pgtype.UUID) (*orgdomain.Membership, error) {
	return nil, errors.New("unexpected call to FindByUserID")
}

func (r *permissionMembershipRepository) FindByUserAndOrganization(
	_ context.Context,
	userID, organizationID pgtype.UUID,
) (*orgdomain.Membership, error) {
	r.findCalls++
	r.findUserID = userID
	r.findOrganizationID = organizationID

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.membership, nil
}

type permittedCall struct {
	reached bool
	code    int
	body    string
}

type permissionIdentity struct {
	set      bool
	identity Identity
}

func authenticatedIn(t *testing.T, userID, organizationID string) permissionIdentity {
	t.Helper()

	return permissionIdentity{
		set: true,
		identity: Identity{
			CurrentUserID:         mustUUID(t, userID),
			CurrentOrganizationID: mustUUID(t, organizationID),
		},
	}
}

func validIdentity(t *testing.T) permissionIdentity {
	t.Helper()

	return authenticatedIn(t, permissionUserID, permissionOrganizationID)
}

func serveWithRequireOrganizationPermission(
	t *testing.T,
	identity permissionIdentity,
	memberships orgdomain.MembershipRepository,
	check PermissionCheck,
) permittedCall {
	t.Helper()

	e := echo.New()

	request := httptest.NewRequest(http.MethodPost, "/patients", nil)
	if identity.set {
		request = request.WithContext(WithIdentity(request.Context(), identity.identity))
	}

	rec := httptest.NewRecorder()
	c := e.NewContext(request, rec)

	result := permittedCall{}

	handler := RequireOrganizationPermission(memberships, orgdomain.AuthorizationPolicy{}, check)(
		func(c echo.Context) error {
			result.reached = true

			return c.NoContent(http.StatusOK)
		},
	)

	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	result.code = rec.Code
	result.body = rec.Body.String()

	assert.NotEqual(t, http.StatusPaymentRequired, result.code,
		"402 is reserved for subscription state, no permission outcome may produce it")

	return result
}

func membershipWithRole(t *testing.T, role orgdomain.Role) *orgdomain.Membership {
	t.Helper()

	var userID, organizationID pgtype.UUID
	require.NoError(t, userID.Scan(permissionUserID))
	require.NoError(t, organizationID.Scan(permissionOrganizationID))

	return &orgdomain.Membership{
		OrganizationId: organizationID,
		UserId:         userID,
		Role:           role,
	}
}

func assertForbiddenBody(t *testing.T, call permittedCall) {
	t.Helper()

	var body map[string]string
	require.NoError(t, json.Unmarshal([]byte(call.body), &body))

	assert.Equal(t, map[string]string{
		"code":    "forbidden",
		"message": "You do not have permission to perform this action.",
	}, body)
}

func TestRequireOrganizationPermissionEnforcesThePolicyTruthTable(t *testing.T) {
	tests := []struct {
		name    string
		check   PermissionCheck
		role    orgdomain.Role
		allowed bool
	}{
		{"manage organization as manager", RequireCanManageOrganization, orgdomain.RoleManager, true},
		{"manage organization as admin", RequireCanManageOrganization, orgdomain.RoleAdmin, true},
		{"manage organization as member", RequireCanManageOrganization, orgdomain.RoleMember, false},

		{"manage members as manager", RequireCanManageMembers, orgdomain.RoleManager, true},
		{"manage members as admin", RequireCanManageMembers, orgdomain.RoleAdmin, true},
		{"manage members as member", RequireCanManageMembers, orgdomain.RoleMember, false},

		// Billing is the one permission that separates Manager from Admin.
		{"manage billing as manager", RequireCanManageBilling, orgdomain.RoleManager, true},
		{"manage billing as admin", RequireCanManageBilling, orgdomain.RoleAdmin, false},
		{"manage billing as member", RequireCanManageBilling, orgdomain.RoleMember, false},

		{"view organization as manager", RequireCanViewOrganization, orgdomain.RoleManager, true},
		{"view organization as admin", RequireCanViewOrganization, orgdomain.RoleAdmin, true},
		{"view organization as member", RequireCanViewOrganization, orgdomain.RoleMember, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memberships := &permissionMembershipRepository{membership: membershipWithRole(t, tt.role)}

			call := serveWithRequireOrganizationPermission(t, validIdentity(t), memberships, tt.check)

			assert.Equal(t, 1, memberships.findCalls, "the membership must be resolved exactly once")

			if tt.allowed {
				assert.True(t, call.reached, "a permitted role must reach the handler")
				assert.Equal(t, http.StatusOK, call.code)

				return
			}

			assert.False(t, call.reached, "a denied role must not reach the handler")
			assert.Equal(t, http.StatusForbidden, call.code)
			assertForbiddenBody(t, call)
		})
	}
}

func TestRequireOrganizationPermissionScopesTheLookupToBothIdentities(t *testing.T) {
	memberships := &permissionMembershipRepository{
		membership: membershipWithRole(t, orgdomain.RoleManager),
	}

	serveWithRequireOrganizationPermission(t, validIdentity(t), memberships, RequireCanManageBilling)

	var expectedUserID, expectedOrganizationID pgtype.UUID
	require.NoError(t, expectedUserID.Scan(permissionUserID))
	require.NoError(t, expectedOrganizationID.Scan(permissionOrganizationID))

	assert.Equal(t, expectedUserID, memberships.findUserID)
	assert.Equal(t, expectedOrganizationID, memberships.findOrganizationID)
}

func TestRequireOrganizationPermissionForbidsANonMember(t *testing.T) {
	memberships := &permissionMembershipRepository{findErr: orgdomain.ErrMembershipNotFound}

	call := serveWithRequireOrganizationPermission(t, validIdentity(t), memberships, RequireCanViewOrganization)

	assert.False(t, call.reached, "a non-member must not reach the handler")
	assert.Equal(t, http.StatusForbidden, call.code)
	assert.NotEqual(t, http.StatusNotFound, call.code, "a non-member is a permission failure, not a 404")

	assertForbiddenBody(t, call)
}

func TestRequireOrganizationPermissionForbidsAWrappedMembershipNotFound(t *testing.T) {
	memberships := &permissionMembershipRepository{
		findErr: errors.Join(errors.New("querying memberships"), orgdomain.ErrMembershipNotFound),
	}

	call := serveWithRequireOrganizationPermission(t, validIdentity(t), memberships, RequireCanViewOrganization)

	assert.False(t, call.reached)
	assert.Equal(t, http.StatusForbidden, call.code)
}

func TestRequireOrganizationPermissionFailsClosedOnARepositoryError(t *testing.T) {
	memberships := &permissionMembershipRepository{findErr: errors.New("postgres: connection refused")}

	call := serveWithRequireOrganizationPermission(t, validIdentity(t), memberships, RequireCanViewOrganization)

	assert.False(t, call.reached, "an unverifiable role must never be treated as permitted")
	assert.Equal(t, http.StatusInternalServerError, call.code)
	assert.NotEqual(t, http.StatusForbidden, call.code,
		"an infrastructure fault is ours, not a permission verdict about the caller")
}

func TestRequireOrganizationPermissionFailsClosedWithoutAnAuthenticatedIdentity(t *testing.T) {
	tests := []struct {
		name     string
		identity permissionIdentity
	}{
		{
			name:     "RequireAuth never ran",
			identity: permissionIdentity{},
		},
		{
			name:     "RequireAuth rejected the request",
			identity: permissionIdentity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memberships := &permissionMembershipRepository{
				membership: membershipWithRole(t, orgdomain.RoleManager),
			}

			call := serveWithRequireOrganizationPermission(t, tt.identity, memberships, RequireCanViewOrganization)

			assert.False(t, call.reached, "a request with no proven identity must not reach the handler")
			assert.Equal(t, http.StatusInternalServerError, call.code)
			assert.Zero(t, memberships.findCalls, "no membership can be resolved without both ids")
		})
	}
}

func TestRequireOrganizationPermissionNeverAnswersPaymentRequired(t *testing.T) {
	tests := []struct {
		name        string
		identity    permissionIdentity
		memberships *permissionMembershipRepository
		check       PermissionCheck
	}{
		{
			name:        "permitted",
			identity:    validIdentity(t),
			memberships: &permissionMembershipRepository{membership: membershipWithRole(t, orgdomain.RoleManager)},
			check:       RequireCanManageBilling,
		},
		{
			name:        "denied",
			identity:    validIdentity(t),
			memberships: &permissionMembershipRepository{membership: membershipWithRole(t, orgdomain.RoleMember)},
			check:       RequireCanManageBilling,
		},
		{
			name:        "not a member",
			identity:    validIdentity(t),
			memberships: &permissionMembershipRepository{findErr: orgdomain.ErrMembershipNotFound},
			check:       RequireCanViewOrganization,
		},
		{
			name:        "repository failure",
			identity:    validIdentity(t),
			memberships: &permissionMembershipRepository{findErr: errors.New("postgres: connection refused")},
			check:       RequireCanViewOrganization,
		},
		{
			name:        "no authenticated identity",
			identity:    permissionIdentity{},
			memberships: &permissionMembershipRepository{},
			check:       RequireCanViewOrganization,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := serveWithRequireOrganizationPermission(t, tt.identity, tt.memberships, tt.check)

			assert.NotEqual(t, http.StatusPaymentRequired, call.code, "402 is reserved for subscription state")
		})
	}
}

func TestRequireOrganizationPermissionPanicsWithoutItsDependencies(t *testing.T) {
	policy := orgdomain.AuthorizationPolicy{}

	t.Run("no membership repository", func(t *testing.T) {
		assert.Panics(t, func() { RequireOrganizationPermission(nil, policy, RequireCanViewOrganization) })
	})

	t.Run("no permission check", func(t *testing.T) {
		assert.Panics(t, func() {
			RequireOrganizationPermission(&permissionMembershipRepository{}, policy, nil)
		})
	})
}
