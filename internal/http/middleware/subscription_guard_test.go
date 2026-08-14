package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const guardOrganizationID = "0f8fad5b-d9cb-469f-a165-708677289501"

type guardOrganizationRepository struct {
	organization *orgdomain.Organization
	findErr      error

	findCalls int
	findID    pgtype.UUID
}

func (r *guardOrganizationRepository) Create(context.Context, *orgdomain.Organization) error {
	return errors.New("unexpected call to Create")
}

func (r *guardOrganizationRepository) FindByID(_ context.Context, id pgtype.UUID) (*orgdomain.Organization, error) {
	r.findCalls++
	r.findID = id

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.organization, nil
}

func (r *guardOrganizationRepository) FindExpiredTrials(context.Context, time.Time, int) ([]*orgdomain.Organization, error) {
	return nil, errors.New("unexpected call to FindExpiredTrials")
}

func (r *guardOrganizationRepository) UpdateSubscriptionStatus(context.Context, pgtype.UUID, orgdomain.SubscriptionStatus) error {
	return errors.New("unexpected call to UpdateSubscriptionStatus")
}

type guardedCall struct {
	reached bool
	code    int
	body    string
}

type organizationOnContext struct {
	set   bool
	value any
}

func authenticated(id string) organizationOnContext {
	return organizationOnContext{set: true, value: id}
}

func serveWithGuard(t *testing.T, identity organizationOnContext, orgs orgdomain.OrganizationRepository) guardedCall {
	t.Helper()

	e := echo.New()

	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/patients", nil), rec)

	if identity.set {
		c.Set(ContextKeyOrganizationID, identity.value)
	}

	result := guardedCall{}

	handler := SubscriptionGuard(orgs, orgdomain.OrganizationAccessPolicy{})(func(c echo.Context) error {
		result.reached = true

		return c.NoContent(http.StatusOK)
	})

	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	result.code = rec.Code
	result.body = rec.Body.String()

	return result
}

func organization(t *testing.T, status orgdomain.SubscriptionStatus, trialEndsAt time.Time) *orgdomain.Organization {
	t.Helper()

	var id pgtype.UUID
	require.NoError(t, id.Scan(guardOrganizationID))

	return &orgdomain.Organization{
		Id:                 id,
		Name:               "Tuenti",
		SubscriptionStatus: status,
		TrialStartsAt:      trialEndsAt.Add(-14 * 24 * time.Hour),
		TrialEndsAt:        trialEndsAt,
	}
}

func TestSubscriptionGuardAllowsOrganizationsThatMayOperate(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		status      orgdomain.SubscriptionStatus
		trialEndsAt time.Time
	}{
		{
			name:        "active subscription",
			status:      orgdomain.Active,
			trialEndsAt: now.Add(-30 * 24 * time.Hour),
		},
		{
			name:        "trial still running",
			status:      orgdomain.Trialing,
			trialEndsAt: now.Add(time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgs := &guardOrganizationRepository{organization: organization(t, tt.status, tt.trialEndsAt)}

			call := serveWithGuard(t, authenticated(guardOrganizationID), orgs)

			assert.True(t, call.reached, "an entitled organization must reach the handler")
			assert.Equal(t, http.StatusOK, call.code)
			assert.Equal(t, 1, orgs.findCalls, "the organization must be loaded exactly once")
		})
	}
}

func TestSubscriptionGuardBlocksOrganizationsThatMayNotOperate(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		status      orgdomain.SubscriptionStatus
		trialEndsAt time.Time
	}{
		{
			name:        "trial ended at this instant",
			status:      orgdomain.Trialing,
			trialEndsAt: now,
		},
		{
			name:        "trial ended earlier",
			status:      orgdomain.Trialing,
			trialEndsAt: now.Add(-time.Hour),
		},
		{
			name:        "suspended",
			status:      orgdomain.Suspended,
			trialEndsAt: now.Add(-time.Hour),
		},
		{
			name:        "past due",
			status:      orgdomain.PastDue,
			trialEndsAt: now.Add(-time.Hour),
		},
		{
			name:        "canceled",
			status:      orgdomain.Canceled,
			trialEndsAt: now.Add(-time.Hour),
		},
		{
			name:        "suspended inside a still-open trial window",
			status:      orgdomain.Suspended,
			trialEndsAt: now.Add(time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgs := &guardOrganizationRepository{organization: organization(t, tt.status, tt.trialEndsAt)}

			call := serveWithGuard(t, authenticated(guardOrganizationID), orgs)

			assert.False(t, call.reached, "a blocked organization must not reach the handler")
			assert.Equal(t, http.StatusPaymentRequired, call.code)

			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(call.body), &body))

			assert.Equal(t, map[string]string{
				"error":               "subscription_required",
				"subscription_status": string(tt.status),
			}, body, "the client must learn why it was blocked and in which state")
		})
	}
}

func TestSubscriptionGuardLoadsTheOrganizationFromTheToken(t *testing.T) {
	orgs := &guardOrganizationRepository{
		organization: organization(t, orgdomain.Active, time.Now().UTC()),
	}

	serveWithGuard(t, authenticated(guardOrganizationID), orgs)

	var expected pgtype.UUID
	require.NoError(t, expected.Scan(guardOrganizationID))

	assert.Equal(t, expected, orgs.findID)
}

func TestSubscriptionGuardRejectsAnUnknownOrganization(t *testing.T) {
	orgs := &guardOrganizationRepository{findErr: orgdomain.ErrOrganizationNotFound}

	call := serveWithGuard(t, authenticated(guardOrganizationID), orgs)

	assert.False(t, call.reached)
	assert.Equal(t, http.StatusNotFound, call.code)
}

func TestSubscriptionGuardFailsClosedOnARepositoryError(t *testing.T) {
	orgs := &guardOrganizationRepository{findErr: errors.New("postgres: connection refused")}

	call := serveWithGuard(t, authenticated(guardOrganizationID), orgs)

	assert.False(t, call.reached, "an unverifiable subscription must never be treated as valid")
	assert.Equal(t, http.StatusInternalServerError, call.code)
}

func TestSubscriptionGuardFailsClosedWithoutAnAuthenticatedOrganization(t *testing.T) {
	tests := []struct {
		name     string
		identity organizationOnContext
	}{
		{
			name:     "RequireAuth never ran",
			identity: organizationOnContext{},
		},
		{
			name:     "RequireAuth rejected the request",
			identity: organizationOnContext{set: true, value: nil},
		},
		{
			name:     "empty organization id",
			identity: authenticated(""),
		},
		{
			name:     "organization id is not a string",
			identity: organizationOnContext{set: true, value: 42},
		},
		{
			name:     "organization id is not a uuid",
			identity: authenticated("not-a-uuid"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgs := &guardOrganizationRepository{
				organization: organization(t, orgdomain.Active, time.Now().UTC()),
			}

			call := serveWithGuard(t, tt.identity, orgs)

			assert.False(t, call.reached, "a request with no proven organization must not reach the handler")
			assert.Equal(t, http.StatusInternalServerError, call.code)
			assert.Zero(t, orgs.findCalls, "no organization can be looked up without an id")
		})
	}
}

func TestSubscriptionGuardNeverAnswersForbidden(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name     string
		identity organizationOnContext
		orgs     *guardOrganizationRepository
	}{
		{
			name:     "active",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{organization: organization(t, orgdomain.Active, now)},
		},
		{
			name:     "trialing",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{organization: organization(t, orgdomain.Trialing, now.Add(time.Hour))},
		},
		{
			name:     "trial expired",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{organization: organization(t, orgdomain.Trialing, now.Add(-time.Hour))},
		},
		{
			name:     "suspended",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{organization: organization(t, orgdomain.Suspended, now)},
		},
		{
			name:     "past due",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{organization: organization(t, orgdomain.PastDue, now)},
		},
		{
			name:     "canceled",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{organization: organization(t, orgdomain.Canceled, now)},
		},
		{
			name:     "organization not found",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{findErr: orgdomain.ErrOrganizationNotFound},
		},
		{
			name:     "repository failure",
			identity: authenticated(guardOrganizationID),
			orgs:     &guardOrganizationRepository{findErr: errors.New("postgres: connection refused")},
		},
		{
			name:     "no authenticated organization",
			identity: organizationOnContext{},
			orgs:     &guardOrganizationRepository{},
		},
		{
			name:     "malformed organization id",
			identity: authenticated("not-a-uuid"),
			orgs:     &guardOrganizationRepository{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := serveWithGuard(t, tt.identity, tt.orgs)

			assert.NotEqual(t, http.StatusForbidden, call.code, "403 is reserved for permission failures")
		})
	}
}

func TestSubscriptionGuardPanicsWithoutARepository(t *testing.T) {
	assert.Panics(t, func() { SubscriptionGuard(nil, orgdomain.OrganizationAccessPolicy{}) })
}
