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

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

const gateUserID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

type gateUserRepository struct {
	user    *identitydomain.User
	findErr error

	findCalls int
	findID    pgtype.UUID
}

func (r *gateUserRepository) Create(context.Context, *identitydomain.User) error {
	return errors.New("unexpected call to Create")
}

func (r *gateUserRepository) FindByEmail(context.Context, string) (*identitydomain.User, error) {
	return nil, errors.New("unexpected call to FindByEmail")
}

func (r *gateUserRepository) FindByID(_ context.Context, id pgtype.UUID) (*identitydomain.User, error) {
	r.findCalls++
	r.findID = id

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.user, nil
}

func (r *gateUserRepository) UpdatePasswordDigest(context.Context, pgtype.UUID, string) error {
	return errors.New("unexpected call to UpdatePasswordDigest")
}

func (r *gateUserRepository) MarkConfirmed(context.Context, pgtype.UUID, time.Time) error {
	return errors.New("unexpected call to MarkConfirmed")
}

type gatedCall struct {
	reached bool
	code    int
	body    string
}

type confirmedEmailIdentity struct {
	set      bool
	identity Identity
}

func signedInAs(t *testing.T, userID string) confirmedEmailIdentity {
	t.Helper()

	return confirmedEmailIdentity{set: true, identity: Identity{CurrentUserID: mustUUID(t, userID)}}
}

func serveWithConfirmationGate(t *testing.T, identity confirmedEmailIdentity, users identitydomain.UserRepository) gatedCall {
	t.Helper()

	e := echo.New()

	request := httptest.NewRequest(http.MethodPost, "/patients", nil)
	if identity.set {
		request = request.WithContext(WithIdentity(request.Context(), identity.identity))
	}

	rec := httptest.NewRecorder()
	c := e.NewContext(request, rec)

	result := gatedCall{}

	handler := RequireConfirmedEmail(users)(func(c echo.Context) error {
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

func gateUser(t *testing.T, confirmedAt *time.Time) *identitydomain.User {
	t.Helper()

	var id pgtype.UUID
	require.NoError(t, id.Scan(gateUserID))

	return &identitydomain.User{
		Id:          id,
		Name:        "Ada Lovelace",
		Email:       "ada@tuenti.test",
		ConfirmedAt: confirmedAt,
	}
}

func confirmedAt(t time.Time) *time.Time { return &t }

func TestRequireConfirmedEmailAllowsConfirmedUsers(t *testing.T) {
	users := &gateUserRepository{user: gateUser(t, confirmedAt(time.Now().UTC()))}

	call := serveWithConfirmationGate(t, signedInAs(t, gateUserID), users)

	assert.True(t, call.reached, "a confirmed user must reach the handler")
	assert.Equal(t, http.StatusOK, call.code)
	assert.Equal(t, 1, users.findCalls, "the user must be loaded exactly once")
}

func TestRequireConfirmedEmailBlocksUnconfirmedUsers(t *testing.T) {
	users := &gateUserRepository{user: gateUser(t, nil)}

	call := serveWithConfirmationGate(t, signedInAs(t, gateUserID), users)

	assert.False(t, call.reached, "an unconfirmed user must not reach the handler")
	assert.Equal(t, http.StatusForbidden, call.code)

	var body map[string]string
	require.NoError(t, json.Unmarshal([]byte(call.body), &body))

	assert.Equal(t, map[string]string{
		"code":    "email_not_confirmed",
		"message": "Please confirm your email address to continue.",
	}, body)
}

func TestRequireConfirmedEmailLoadsTheUserFromTheToken(t *testing.T) {
	users := &gateUserRepository{user: gateUser(t, confirmedAt(time.Now().UTC()))}

	serveWithConfirmationGate(t, signedInAs(t, gateUserID), users)

	var expected pgtype.UUID
	require.NoError(t, expected.Scan(gateUserID))

	assert.Equal(t, expected, users.findID)
}

func TestRequireConfirmedEmailFailsClosedOnAnUnknownUser(t *testing.T) {
	users := &gateUserRepository{findErr: identitydomain.ErrUserNotFound}

	call := serveWithConfirmationGate(t, signedInAs(t, gateUserID), users)

	assert.False(t, call.reached)
	assert.Equal(t, http.StatusInternalServerError, call.code)
	assert.NotEqual(t, http.StatusNotFound, call.code, "a missing user is a data-integrity fault, not a 404")
}

func TestRequireConfirmedEmailFailsClosedOnARepositoryError(t *testing.T) {
	users := &gateUserRepository{findErr: errors.New("postgres: connection refused")}

	call := serveWithConfirmationGate(t, signedInAs(t, gateUserID), users)

	assert.False(t, call.reached, "an unverifiable confirmation state must never be treated as confirmed")
	assert.Equal(t, http.StatusInternalServerError, call.code)
}

func TestRequireConfirmedEmailFailsClosedWithoutAnAuthenticatedUser(t *testing.T) {
	tests := []struct {
		name     string
		identity confirmedEmailIdentity
	}{
		{
			name:     "RequireAuth never ran",
			identity: confirmedEmailIdentity{},
		},
		{
			name:     "RequireAuth rejected the request",
			identity: confirmedEmailIdentity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &gateUserRepository{user: gateUser(t, confirmedAt(time.Now().UTC()))}

			call := serveWithConfirmationGate(t, tt.identity, users)

			assert.False(t, call.reached, "a request with no proven user must not reach the handler")
			assert.Equal(t, http.StatusInternalServerError, call.code)
			assert.Zero(t, users.findCalls, "no user can be looked up without an id")
		})
	}
}

func TestRequireConfirmedEmailNeverAnswersPaymentRequired(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name     string
		identity confirmedEmailIdentity
		users    *gateUserRepository
	}{
		{
			name:     "confirmed",
			identity: signedInAs(t, gateUserID),
			users:    &gateUserRepository{user: gateUser(t, confirmedAt(now))},
		},
		{
			name:     "unconfirmed",
			identity: signedInAs(t, gateUserID),
			users:    &gateUserRepository{user: gateUser(t, nil)},
		},
		{
			name:     "user not found",
			identity: signedInAs(t, gateUserID),
			users:    &gateUserRepository{findErr: identitydomain.ErrUserNotFound},
		},
		{
			name:     "repository failure",
			identity: signedInAs(t, gateUserID),
			users:    &gateUserRepository{findErr: errors.New("postgres: connection refused")},
		},
		{
			name:     "no authenticated user",
			identity: confirmedEmailIdentity{},
			users:    &gateUserRepository{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := serveWithConfirmationGate(t, tt.identity, tt.users)

			assert.NotEqual(t, http.StatusPaymentRequired, call.code, "402 is reserved for subscription state")
		})
	}
}

func TestRequireConfirmedEmailPanicsWithoutARepository(t *testing.T) {
	assert.Panics(t, func() { RequireConfirmedEmail(nil) })
}
