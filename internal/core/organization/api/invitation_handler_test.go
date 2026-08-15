package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
)

const (
	inviterUserId       = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
	invitationId        = "1b4e28ba-2fa1-11d2-883f-0016d3cca427"
	membershipId        = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	otherOrganizationId = "9f8fad5b-d9cb-469f-a165-708677289509"
)

type fakeCreateInvitationUseCase struct {
	invitation *domain.Invitation
	err        error

	calls          int
	inviterUserID  pgtype.UUID
	organizationID pgtype.UUID
	email          string
	role           domain.Role
}

func (f *fakeCreateInvitationUseCase) CreateInvitation(
	_ context.Context,
	inviterUserID, organizationID pgtype.UUID,
	email string,
	role domain.Role,
) (*domain.Invitation, error) {
	f.calls++
	f.inviterUserID = inviterUserID
	f.organizationID = organizationID
	f.email = email
	f.role = role

	if f.err != nil {
		return nil, f.err
	}

	return f.invitation, nil
}

type fakeListInvitationsUseCase struct {
	invitations []*domain.Invitation
	err         error

	calls     int
	requested pgtype.UUID
}

func (f *fakeListInvitationsUseCase) ListInvitations(
	_ context.Context,
	organizationID pgtype.UUID,
) ([]*domain.Invitation, error) {
	f.calls++
	f.requested = organizationID

	if f.err != nil {
		return nil, f.err
	}

	return f.invitations, nil
}

type fakeRevokeInvitationUseCase struct {
	err error

	calls          int
	revokerUserID  pgtype.UUID
	organizationID pgtype.UUID
	invitationID   pgtype.UUID
}

func (f *fakeRevokeInvitationUseCase) RevokeInvitation(
	_ context.Context,
	revokerUserID, organizationID, invitationID pgtype.UUID,
) error {
	f.calls++
	f.revokerUserID = revokerUserID
	f.organizationID = organizationID
	f.invitationID = invitationID

	return f.err
}

type fakeAcceptInvitationUseCase struct {
	membership *domain.Membership
	err        error

	calls    int
	token    string
	name     *string
	password *string
}

func (f *fakeAcceptInvitationUseCase) AcceptInvitation(
	_ context.Context,
	token string,
	name, password *string,
) (*domain.Membership, error) {
	f.calls++
	f.token = token
	f.name = name
	f.password = password

	if f.err != nil {
		return nil, f.err
	}

	return f.membership, nil
}

type authContext struct {
	set            bool
	userID         string
	organizationID string
}

func authenticatedAs(userID, organizationID string) authContext {
	return authContext{set: true, userID: userID, organizationID: organizationID}
}

func (a authContext) apply(c echo.Context) {
	if !a.set {
		return
	}

	c.Set(m.ContextKeyUserID, a.userID)
	c.Set(m.ContextKeyOrganizationID, a.organizationID)
}

func newInvitationRequest(method, body string) *http.Request {
	req := httptest.NewRequest(method, "/invitations", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	return req
}

func callCreateInvitation(
	t *testing.T,
	usecase *fakeCreateInvitationUseCase,
	organizationID string,
	auth authContext,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(newInvitationRequest(http.MethodPost, body), rec)
	c.SetPath("/organizations/:organizationId/invitations")
	c.SetParamNames("organizationId")
	c.SetParamValues(organizationID)
	auth.apply(c)

	handler := NewInvitationHandler(usecase, nil, nil, nil)

	if err := handler.Create(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func callListInvitations(
	t *testing.T,
	usecase *fakeListInvitationsUseCase,
	organizationID string,
	auth authContext,
) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(newInvitationRequest(http.MethodGet, ""), rec)
	c.SetPath("/organizations/:organizationId/invitations")
	c.SetParamNames("organizationId")
	c.SetParamValues(organizationID)
	auth.apply(c)

	handler := NewInvitationHandler(nil, nil, nil, usecase)

	if err := handler.List(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func callRevokeInvitation(
	t *testing.T,
	usecase *fakeRevokeInvitationUseCase,
	organizationID, id string,
	auth authContext,
) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(newInvitationRequest(http.MethodDelete, ""), rec)
	c.SetPath("/organizations/:organizationId/invitations/:id")
	c.SetParamNames("organizationId", "id")
	c.SetParamValues(organizationID, id)
	auth.apply(c)

	handler := NewInvitationHandler(nil, nil, usecase, nil)

	if err := handler.Revoke(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func callAcceptInvitation(
	t *testing.T,
	usecase *fakeAcceptInvitationUseCase,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(newInvitationRequest(http.MethodPost, body), rec)
	c.SetPath("/invitations/accept")

	handler := NewInvitationHandler(nil, usecase, nil, nil)

	if err := handler.Accept(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func pendingInvitation(t *testing.T) *domain.Invitation {
	t.Helper()

	return &domain.Invitation{
		Id:                    mustUUID(t, invitationId),
		OrganizationId:        mustUUID(t, organizationId),
		Email:                 "wile.coyote@example.com",
		Role:                  domain.RoleMember,
		TokenDigest:           "a-digest-that-must-never-be-served",
		InvitedByMembershipId: mustUUID(t, membershipId),
		CreatedAt:             time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		ExpiresAt:             time.Now().UTC().Add(24 * time.Hour),
	}
}

func TestCreateInvitationReturnsTheIssuedInvitation(t *testing.T) {
	invitation := pendingInvitation(t)
	usecase := &fakeCreateInvitationUseCase{invitation: invitation}

	rec := callCreateInvitation(t, usecase, organizationId,
		authenticatedAs(inviterUserId, organizationId),
		`{"email":"wile.coyote@example.com","role":"member"}`)

	require.Equal(t, http.StatusCreated, rec.Code)

	var body struct {
		Id                    string  `json:"id"`
		Email                 string  `json:"email"`
		Role                  string  `json:"role"`
		Status                string  `json:"status"`
		InvitedByMembershipId string  `json:"invited_by_membership_id"`
		AcceptedAt            *string `json:"accepted_at"`
		RevokedAt             *string `json:"revoked_at"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	assert.Equal(t, invitationId, body.Id)
	assert.Equal(t, "wile.coyote@example.com", body.Email)
	assert.Equal(t, "member", body.Role)
	assert.Equal(t, "pending", body.Status)
	assert.Equal(t, membershipId, body.InvitedByMembershipId)
	assert.Nil(t, body.AcceptedAt)
	assert.Nil(t, body.RevokedAt)

	assert.NotContains(t, rec.Body.String(), "a-digest-that-must-never-be-served",
		"the token digest must never reach a client")
	assert.NotContains(t, rec.Body.String(), "token_digest")

	require.Equal(t, 1, usecase.calls)
	assert.Equal(t, mustUUID(t, inviterUserId), usecase.inviterUserID, "the inviter comes from the token, not the body")
	assert.Equal(t, mustUUID(t, organizationId), usecase.organizationID)
	assert.Equal(t, "wile.coyote@example.com", usecase.email)
	assert.Equal(t, domain.RoleMember, usecase.role)
}

func TestCreateInvitationRejectsAMalformedOrganizationId(t *testing.T) {
	usecase := &fakeCreateInvitationUseCase{}

	rec := callCreateInvitation(t, usecase, "not-a-uuid",
		authenticatedAs(inviterUserId, organizationId),
		`{"email":"wile.coyote@example.com","role":"member"}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"message":"invalid organization id"}`, rec.Body.String())
	assert.Zero(t, usecase.calls, "a rejected id must never reach the use case")
}

func TestCreateInvitationFailsClosedWithoutAnAuthenticatedContext(t *testing.T) {
	tests := []struct {
		name string
		auth authContext
	}{
		{name: "RequireAuth never ran", auth: authContext{}},
		{name: "user missing", auth: authContext{set: true, userID: "", organizationID: organizationId}},
		{name: "organization missing", auth: authContext{set: true, userID: inviterUserId, organizationID: ""}},
		{name: "user not a uuid", auth: authContext{set: true, userID: "nope", organizationID: organizationId}},
		{name: "organization not a uuid", auth: authContext{set: true, userID: inviterUserId, organizationID: "nope"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeCreateInvitationUseCase{}

			rec := callCreateInvitation(t, usecase, organizationId, test.auth,
				`{"email":"wile.coyote@example.com","role":"member"}`)

			require.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.JSONEq(t, `{"message":"internal server error"}`, rec.Body.String())
			assert.Zero(t, usecase.calls, "an unauthenticated request must never reach the use case")
		})
	}
}

func TestCreateInvitationRejectsAnOrganizationTheTokenDoesNotName(t *testing.T) {
	usecase := &fakeCreateInvitationUseCase{}

	rec := callCreateInvitation(t, usecase, otherOrganizationId,
		authenticatedAs(inviterUserId, organizationId),
		`{"email":"wile.coyote@example.com","role":"member"}`)

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"message":"organization mismatch"}`, rec.Body.String())
	assert.Zero(t, usecase.calls, "a cross-organization request must never reach the use case")
}

func TestCreateInvitationRejectsAnUnreadableBody(t *testing.T) {
	usecase := &fakeCreateInvitationUseCase{}

	rec := callCreateInvitation(t, usecase, organizationId,
		authenticatedAs(inviterUserId, organizationId), `{"email":`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"message":"invalid request body"}`, rec.Body.String())
	assert.Zero(t, usecase.calls)
}

func TestCreateInvitationRejectsInvalidPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing email", body: `{"role":"member"}`},
		{name: "malformed email", body: `{"email":"not-an-email","role":"member"}`},
		{name: "missing role", body: `{"email":"wile.coyote@example.com"}`},
		{name: "unknown role", body: `{"email":"wile.coyote@example.com","role":"owner"}`},
		{name: "empty payload", body: `{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeCreateInvitationUseCase{}

			rec := callCreateInvitation(t, usecase, organizationId,
				authenticatedAs(inviterUserId, organizationId), test.body)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Zero(t, usecase.calls, "an invalid payload must never reach the use case")
		})
	}
}

func TestCreateInvitationMapsUseCaseFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "forbidden", err: domain.ErrInvitationForbidden, code: http.StatusForbidden},
		{name: "not a member", err: domain.ErrMembershipNotFound, code: http.StatusForbidden},
		{name: "wrapped forbidden", err: fmt.Errorf("authorize: %w", domain.ErrInvitationForbidden), code: http.StatusForbidden},
		{name: "already a member", err: domain.ErrAlreadyMember, code: http.StatusConflict},
		{name: "duplicate invitation", err: domain.ErrDuplicateInvitation, code: http.StatusConflict},
		{name: "wrapped duplicate", err: fmt.Errorf("insert: %w", domain.ErrDuplicateInvitation), code: http.StatusConflict},
		{name: "infrastructure failure", err: errors.New("db down: dial tcp 10.0.0.7:5432"), code: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeCreateInvitationUseCase{err: test.err}

			rec := callCreateInvitation(t, usecase, organizationId,
				authenticatedAs(inviterUserId, organizationId),
				`{"email":"wile.coyote@example.com","role":"member"}`)

			require.Equal(t, test.code, rec.Code)
			assert.Equal(t, 1, usecase.calls)
			assert.NotContains(t, rec.Body.String(), "10.0.0.7", "infrastructure detail must not reach the client")
		})
	}
}

func TestListInvitationsReturnsEveryInvitationWithADerivedStatus(t *testing.T) {
	accepted := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	revoked := time.Date(2026, time.February, 2, 0, 0, 0, 0, time.UTC)

	pending := pendingInvitation(t)

	acceptedInvitation := pendingInvitation(t)
	acceptedInvitation.AcceptedAt = &accepted

	revokedInvitation := pendingInvitation(t)
	revokedInvitation.RevokedAt = &revoked

	expiredInvitation := pendingInvitation(t)
	expiredInvitation.ExpiresAt = time.Now().UTC().Add(-time.Hour)

	usecase := &fakeListInvitationsUseCase{
		invitations: []*domain.Invitation{pending, acceptedInvitation, revokedInvitation, expiredInvitation},
	}

	rec := callListInvitations(t, usecase, organizationId, authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusOK, rec.Code)

	var body []struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 4)

	assert.Equal(t, "pending", body[0].Status)
	assert.Equal(t, "accepted", body[1].Status)
	assert.Equal(t, "revoked", body[2].Status)
	assert.Equal(t, "expired", body[3].Status)

	assert.NotContains(t, rec.Body.String(), "a-digest-that-must-never-be-served")

	require.Equal(t, 1, usecase.calls)
	assert.Equal(t, mustUUID(t, organizationId), usecase.requested)
}

func TestListInvitationsRendersAnEmptyOrganizationAsAnEmptyArray(t *testing.T) {
	usecase := &fakeListInvitationsUseCase{invitations: []*domain.Invitation{}}

	rec := callListInvitations(t, usecase, organizationId, authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()), "an empty list must serialise as [], never null")
}

func TestListInvitationsRendersANilSliceAsAnEmptyArray(t *testing.T) {
	usecase := &fakeListInvitationsUseCase{}

	rec := callListInvitations(t, usecase, organizationId, authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
}

func TestListInvitationsRejectsAMalformedOrganizationId(t *testing.T) {
	usecase := &fakeListInvitationsUseCase{}

	rec := callListInvitations(t, usecase, "not-a-uuid", authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"message":"invalid organization id"}`, rec.Body.String())
	assert.Zero(t, usecase.calls)
}

func TestListInvitationsFailsClosedWithoutAnAuthenticatedContext(t *testing.T) {
	usecase := &fakeListInvitationsUseCase{}

	rec := callListInvitations(t, usecase, organizationId, authContext{})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Zero(t, usecase.calls)
}

func TestListInvitationsRejectsAnOrganizationTheTokenDoesNotName(t *testing.T) {
	usecase := &fakeListInvitationsUseCase{}

	rec := callListInvitations(t, usecase, otherOrganizationId, authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"message":"organization mismatch"}`, rec.Body.String())
	assert.Zero(t, usecase.calls)
}

func TestListInvitationsMapsFailuresTo500(t *testing.T) {
	usecase := &fakeListInvitationsUseCase{err: errors.New("db down: dial tcp 10.0.0.7:5432")}

	rec := callListInvitations(t, usecase, organizationId, authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"message":"internal server error"}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "10.0.0.7")
}

func TestRevokeInvitationReturnsNoContent(t *testing.T) {
	usecase := &fakeRevokeInvitationUseCase{}

	rec := callRevokeInvitation(t, usecase, organizationId, invitationId,
		authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())

	require.Equal(t, 1, usecase.calls)
	assert.Equal(t, mustUUID(t, inviterUserId), usecase.revokerUserID)
	assert.Equal(t, mustUUID(t, organizationId), usecase.organizationID)
	assert.Equal(t, mustUUID(t, invitationId), usecase.invitationID)
}

func TestRevokeInvitationRejectsMalformedIds(t *testing.T) {
	tests := []struct {
		name           string
		organizationID string
		id             string
		want           string
	}{
		{name: "organization id", organizationID: "not-a-uuid", id: invitationId, want: "invalid organization id"},
		{name: "invitation id", organizationID: organizationId, id: "not-a-uuid", want: "invalid invitation id"},
		{name: "empty invitation id", organizationID: organizationId, id: "", want: "invalid invitation id"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeRevokeInvitationUseCase{}

			rec := callRevokeInvitation(t, usecase, test.organizationID, test.id,
				authenticatedAs(inviterUserId, organizationId))

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, `{"message":"`+test.want+`"}`, rec.Body.String())
			assert.Zero(t, usecase.calls)
		})
	}
}

func TestRevokeInvitationFailsClosedWithoutAnAuthenticatedContext(t *testing.T) {
	usecase := &fakeRevokeInvitationUseCase{}

	rec := callRevokeInvitation(t, usecase, organizationId, invitationId, authContext{})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Zero(t, usecase.calls)
}

func TestRevokeInvitationRejectsAnOrganizationTheTokenDoesNotName(t *testing.T) {
	usecase := &fakeRevokeInvitationUseCase{}

	rec := callRevokeInvitation(t, usecase, otherOrganizationId, invitationId,
		authenticatedAs(inviterUserId, organizationId))

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"message":"organization mismatch"}`, rec.Body.String())
	assert.Zero(t, usecase.calls)
}

func TestRevokeInvitationMapsUseCaseFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "not found", err: domain.ErrInvitationNotFound, code: http.StatusNotFound},
		{name: "wrapped not found", err: fmt.Errorf("find: %w", domain.ErrInvitationNotFound), code: http.StatusNotFound},
		{name: "forbidden", err: domain.ErrInvitationForbidden, code: http.StatusForbidden},
		{name: "not a member", err: domain.ErrMembershipNotFound, code: http.StatusForbidden},
		{name: "infrastructure failure", err: errors.New("db down: dial tcp 10.0.0.7:5432"), code: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeRevokeInvitationUseCase{err: test.err}

			rec := callRevokeInvitation(t, usecase, organizationId, invitationId,
				authenticatedAs(inviterUserId, organizationId))

			require.Equal(t, test.code, rec.Code)
			assert.Equal(t, 1, usecase.calls)
			assert.NotContains(t, rec.Body.String(), "10.0.0.7")
		})
	}
}

func TestAcceptInvitationReturnsTheMembership(t *testing.T) {
	usecase := &fakeAcceptInvitationUseCase{
		membership: &domain.Membership{
			Id:             mustUUID(t, membershipId),
			OrganizationId: mustUUID(t, organizationId),
			UserId:         mustUUID(t, inviterUserId),
			Role:           domain.RoleMember,
			CreatedAt:      time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt:      time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		},
	}

	rec := callAcceptInvitation(t, usecase, `{"token":"raw-invitation-token"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"id": "`+membershipId+`",
		"organization_id": "`+organizationId+`",
		"user_id": "`+inviterUserId+`",
		"role": "member"
	}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "created_at", "the response must expose only the MembershipResponse fields")

	require.Equal(t, 1, usecase.calls)
	assert.Equal(t, "raw-invitation-token", usecase.token)
	assert.Nil(t, usecase.name)
	assert.Nil(t, usecase.password)
}

func TestAcceptInvitationForwardsTheSignupFields(t *testing.T) {
	usecase := &fakeAcceptInvitationUseCase{
		membership: &domain.Membership{
			Id:             mustUUID(t, membershipId),
			OrganizationId: mustUUID(t, organizationId),
			UserId:         mustUUID(t, inviterUserId),
			Role:           domain.RoleAdmin,
		},
	}

	rec := callAcceptInvitation(t, usecase,
		`{"token":"raw-invitation-token","name":"Wile E. Coyote","password":"supersecret"}`)

	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, usecase.calls)
	require.NotNil(t, usecase.name)
	require.NotNil(t, usecase.password)
	assert.Equal(t, "Wile E. Coyote", *usecase.name)
	assert.Equal(t, "supersecret", *usecase.password)
}

func TestAcceptInvitationRejectsAnUnreadableBody(t *testing.T) {
	usecase := &fakeAcceptInvitationUseCase{}

	rec := callAcceptInvitation(t, usecase, `{"token":`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"message":"invalid request body"}`, rec.Body.String())
	assert.Zero(t, usecase.calls)
}

func TestAcceptInvitationRejectsInvalidPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing token", body: `{}`},
		{name: "empty token", body: `{"token":""}`},
		{name: "empty name", body: `{"token":"raw","name":""}`},
		{name: "short password", body: `{"token":"raw","name":"Wile","password":"short"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeAcceptInvitationUseCase{}

			rec := callAcceptInvitation(t, usecase, test.body)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Zero(t, usecase.calls, "an invalid payload must never reach the use case")
		})
	}
}

func TestAcceptInvitationMapsUseCaseFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "unknown token", err: domain.ErrInvitationNotFound, code: http.StatusNotFound},
		{name: "expired", err: domain.ErrInvitationExpired, code: http.StatusGone},
		{name: "revoked", err: domain.ErrInvitationRevoked, code: http.StatusGone},
		{name: "already accepted", err: domain.ErrInvitationAlreadyAccepted, code: http.StatusGone},
		{name: "wrapped expired", err: fmt.Errorf("validate: %w", domain.ErrInvitationExpired), code: http.StatusGone},
		{name: "signup fields required", err: domain.ErrInvitationSignupFieldsRequired, code: http.StatusUnprocessableEntity},
		{name: "email already registered", err: domain.ErrInvitationEmailAlreadyRegistered, code: http.StatusConflict},
		{name: "infrastructure failure", err: errors.New("db down: dial tcp 10.0.0.7:5432"), code: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeAcceptInvitationUseCase{err: test.err}

			rec := callAcceptInvitation(t, usecase, `{"token":"raw-invitation-token"}`)

			require.Equal(t, test.code, rec.Code)
			assert.Equal(t, 1, usecase.calls)
			assert.NotContains(t, rec.Body.String(), "10.0.0.7", "infrastructure detail must not reach the client")
			assert.NotContains(t, rec.Body.String(), "raw-invitation-token", "the token must never be echoed back")
		})
	}
}
