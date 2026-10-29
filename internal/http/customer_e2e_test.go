package http_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	catalogapi "github.com/michelsazevedo/tuenti/internal/core/catalog/api"
	customerapi "github.com/michelsazevedo/tuenti/internal/core/customer/api"
	customerapp "github.com/michelsazevedo/tuenti/internal/core/customer/application"
	customerpersistence "github.com/michelsazevedo/tuenti/internal/core/customer/persistence"
	identityapi "github.com/michelsazevedo/tuenti/internal/core/identity/api"
	identityapp "github.com/michelsazevedo/tuenti/internal/core/identity/application"
	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	identitypersistence "github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	orgapi "github.com/michelsazevedo/tuenti/internal/core/organization/api"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	orgpersistence "github.com/michelsazevedo/tuenti/internal/core/organization/persistence"
	apphttp "github.com/michelsazevedo/tuenti/internal/http"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	customerE2ETimeout  = 10 * time.Second
	customerE2EPassword = "C0rrect-Horse-Battery"
)

// customerE2EEnv drives the real router: real signin, real RequireAuth and
// RequireOrganizationPermission middleware, the real customer stack against
// Postgres, and the application's shared HTTPErrorHandler.
type customerE2EEnv struct {
	server       *echo.Echo
	pool         *pgxpool.Pool
	users        *identitypersistence.UserRepository
	refreshStore identitydomain.RefreshTokenStore
}

func newCustomerE2EEnv(t *testing.T) *customerE2EEnv {
	t.Helper()

	conf := customerE2EConfig(t)

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), customerE2ETimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	client := goredis.NewClient(&goredis.Options{Addr: conf.GetRedisAddr(), Password: conf.Redis.Password})
	t.Cleanup(func() { _ = client.Close() })

	pool := pgConn.Pool()
	users := identitypersistence.NewUserRepository(pool)
	refreshStore := identitypersistence.NewRefreshTokenStore(client)
	memberships := orgpersistence.NewMembershipRepository(pool)
	customers := customerpersistence.NewCustomerRepository(pool)

	authz := identityapi.NewAuthzHandler(
		nil,
		identityapp.NewSignin(users, memberships, refreshStore, conf),
		nil, nil, nil, nil, nil, nil,
	)

	customer := customerapi.NewCustomerHandler(
		customerapp.NewCreateCustomer(customers),
		customerapp.NewGetCustomer(customers),
		customerapp.NewListCustomers(customers),
		customerapp.NewUpdateCustomer(customers),
		customerapp.NewDeleteCustomer(customers),
	)

	server := echo.New()
	server.HTTPErrorHandler = apphttp.HTTPErrorHandler

	apphttp.RegisterRoutes(
		server,
		client,
		conf,
		memberships,
		apphttp.NewHealthzHandler(),
		authz,
		orgapi.NewOrganizationHandler(nil),
		orgapi.NewInvitationHandler(nil, nil, nil, nil),
		catalogapi.NewItemHandler(nil, nil, nil, nil, nil),
		customer,
	)

	return &customerE2EEnv{server: server, pool: pool, users: users, refreshStore: refreshStore}
}

func customerE2EConfig(t *testing.T) *config.Config {
	t.Helper()

	defaults := map[string]string{
		"POSTGRES_HOST":               "localhost:5432",
		"POSTGRES_USER":               "tuenti",
		"POSTGRES_PASSWORD":           "tuentipwd",
		"POSTGRES_DB":                 "tuenti",
		"REDIS_HOST":                  "localhost:6379",
		"PASSWORD_RESET_BASE_URL":     "http://localhost:3000/reset-password",
		"EMAIL_CONFIRMATION_BASE_URL": "http://localhost:3000/confirm-email",
		"INVITATION_BASE_URL":         "http://localhost:3000/invitations",
	}

	for key, value := range defaults {
		if _, ok := os.LookupEnv(key); !ok {
			t.Setenv(key, value)
		}
	}

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	return conf
}

func (env *customerE2EEnv) context(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), customerE2ETimeout)
	t.Cleanup(cancel)

	return ctx
}

// newSignedInTenant seeds an organization with one member holding role, signs
// that member in through POST /auth/signin and returns the access token.
func (env *customerE2EEnv) newSignedInTenant(t *testing.T, role orgdomain.Role) string {
	t.Helper()

	return env.signInNewMember(t, env.seedOrganization(t), role)
}

// seedOrganization creates an organization on a trial subscription and
// removes it, with every customer and membership in it, once the test ends.
func (env *customerE2EEnv) seedOrganization(t *testing.T) pgtype.UUID {
	t.Helper()

	ctx := env.context(t)
	suffix := customerE2ESuffix(t)

	var industryID pgtype.UUID
	require.NoError(t, env.pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Customer E2E Industry "+suffix, "customer_e2e_industry_"+suffix,
	).Scan(&industryID))

	now := time.Now().UTC()
	org := &orgdomain.Organization{
		Name:               "Acme " + suffix,
		IndustryID:         industryID,
		NumberOfEmployees:  10,
		TrialStartsAt:      now,
		TrialEndsAt:        now.Add(14 * 24 * time.Hour),
		SubscriptionStatus: orgdomain.Trialing,
	}
	require.NoError(t, orgpersistence.NewOrganizationRepository(env.pool).Create(ctx, org))

	t.Cleanup(func() {
		env.cleanup(t, []customerE2ECleanup{
			{`DELETE FROM customers WHERE organization_id = $1`, org.Id},
			{`DELETE FROM memberships WHERE organization_id = $1`, org.Id},
			{`DELETE FROM organizations WHERE id = $1`, org.Id},
			{`DELETE FROM industries WHERE id = $1`, industryID},
		})
	})

	return org.Id
}

// signInNewMember adds a new user to organizationID with role, signs them in
// through POST /auth/signin and returns the access token.
func (env *customerE2EEnv) signInNewMember(t *testing.T, organizationID pgtype.UUID, role orgdomain.Role) string {
	t.Helper()

	ctx := env.context(t)
	suffix := customerE2ESuffix(t)

	digest, err := bcrypt.GenerateFromPassword([]byte(customerE2EPassword), bcrypt.MinCost)
	require.NoError(t, err)

	user := &identitydomain.User{
		Name:           "Wile E. Coyote " + suffix,
		Email:          "wile." + suffix + "@example.com",
		PasswordDigest: string(digest),
	}
	require.NoError(t, env.users.Create(ctx, user))

	require.NoError(t, orgpersistence.NewMembershipRepository(env.pool).Create(ctx, &orgdomain.Membership{
		OrganizationId: organizationID,
		UserId:         user.Id,
		Role:           role,
	}))

	// Registered after seedOrganization's cleanup, so it runs first (LIFO):
	// the user is gone before its organization is.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), customerE2ETimeout)
		defer cancel()

		if err := env.refreshStore.RevokeAllForUser(cleanupCtx, user.Id.String()); err != nil {
			t.Errorf("cleanup failed for the sessions of user %v: %v", user.Id, err)
		}

		env.cleanup(t, []customerE2ECleanup{
			{`DELETE FROM memberships WHERE user_id = $1`, user.Id},
			{`DELETE FROM users WHERE id = $1`, user.Id},
		})
	})

	signin := env.call(t, http.MethodPost, "/auth/signin", "",
		`{"email":"`+user.Email+`","password":"`+customerE2EPassword+`"}`)
	require.Equal(t, http.StatusOK, signin.Code, "body: %s", signin.Body.String())

	var tokens identityapi.TokenResponse
	require.NoError(t, json.Unmarshal(signin.Body.Bytes(), &tokens))
	require.NotEmpty(t, tokens.AccessToken)

	return tokens.AccessToken
}

type customerE2ECleanup struct {
	sql string
	id  pgtype.UUID
}

func (env *customerE2EEnv) cleanup(t *testing.T, statements []customerE2ECleanup) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), customerE2ETimeout)
	defer cancel()

	for _, statement := range statements {
		if _, err := env.pool.Exec(ctx, statement.sql, statement.id); err != nil {
			t.Errorf("cleanup failed for %q: %v", statement.sql, err)
		}
	}
}

func (env *customerE2EEnv) call(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	if token != "" {
		request.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	env.server.ServeHTTP(rec, request)

	return rec
}

func customerE2ESuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

type customerE2EBody struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	Address        *struct {
		Country string `json:"country"`
	} `json:"address"`
}

func decodeCustomer(t *testing.T, rec *httptest.ResponseRecorder) customerE2EBody {
	t.Helper()

	var body customerE2EBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body: %s", rec.Body.String())

	return body
}

func decodeCustomers(t *testing.T, rec *httptest.ResponseRecorder) []customerE2EBody {
	t.Helper()

	var body []customerE2EBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body: %s", rec.Body.String())

	return body
}

func customerIDs(customers []customerE2EBody) []string {
	ids := make([]string, 0, len(customers))
	for _, customer := range customers {
		ids = append(ids, customer.ID)
	}

	return ids
}

const customerE2ECreateBody = `{
	"name": "Road Runner",
	"company_name": "Acme Corporation",
	"email": "road.runner@example.com",
	"phone_number": "+1 555 0100",
	"address": {"street": "1 Desert Rd", "city": "Mesa", "state": "AZ", "country": "us"}
}`

const customerE2EUpdateBody = `{
	"name": "Road Runner Jr.",
	"company_name": "Acme Corporation",
	"email": "road.runner.jr@example.com",
	"phone_number": "+1 555 0101"
}`

func TestCustomerLifecycleThroughTheRouter(t *testing.T) {
	env := newCustomerE2EEnv(t)
	token := env.newSignedInTenant(t, orgdomain.RoleManager)

	created := env.call(t, http.MethodPost, "/customers", token, customerE2ECreateBody)
	require.Equal(t, http.StatusCreated, created.Code, "body: %s", created.Body.String())

	customer := decodeCustomer(t, created)
	require.NotEmpty(t, customer.ID)
	require.NotNil(t, customer.Address)
	assert.Equal(t, "US", customer.Address.Country, "the domain normalizes the country code")

	listed := env.call(t, http.MethodGet, "/customers", token, "")
	require.Equal(t, http.StatusOK, listed.Code, "body: %s", listed.Body.String())
	assert.Equal(t, []string{customer.ID}, customerIDs(decodeCustomers(t, listed)))

	fetched := env.call(t, http.MethodGet, "/customers/"+customer.ID, token, "")
	require.Equal(t, http.StatusOK, fetched.Code, "body: %s", fetched.Body.String())
	assert.Equal(t, "Road Runner", decodeCustomer(t, fetched).Name)

	updated := env.call(t, http.MethodPut, "/customers/"+customer.ID, token, customerE2EUpdateBody)
	require.Equal(t, http.StatusOK, updated.Code, "body: %s", updated.Body.String())

	afterUpdate := decodeCustomer(t, updated)
	assert.Equal(t, "Road Runner Jr.", afterUpdate.Name)
	assert.Equal(t, "road.runner.jr@example.com", afterUpdate.Email)
	assert.Nil(t, afterUpdate.Address, "a full replace without an address clears it")

	deleted := env.call(t, http.MethodDelete, "/customers/"+customer.ID, token, "")
	require.Equal(t, http.StatusNoContent, deleted.Code, "body: %s", deleted.Body.String())

	gone := env.call(t, http.MethodGet, "/customers/"+customer.ID, token, "")
	assert.Equal(t, http.StatusNotFound, gone.Code, "body: %s", gone.Body.String())

	emptied := env.call(t, http.MethodGet, "/customers", token, "")
	require.Equal(t, http.StatusOK, emptied.Code)
	assert.Empty(t, decodeCustomers(t, emptied), "a soft-deleted customer must drop out of the list")
}

func TestCustomerIsInvisibleToAnotherOrganization(t *testing.T) {
	env := newCustomerE2EEnv(t)
	tokenA := env.newSignedInTenant(t, orgdomain.RoleManager)
	tokenB := env.newSignedInTenant(t, orgdomain.RoleManager)

	created := env.call(t, http.MethodPost, "/customers", tokenA, customerE2ECreateBody)
	require.Equal(t, http.StatusCreated, created.Code, "body: %s", created.Body.String())
	customerID := decodeCustomer(t, created).ID

	probes := []struct {
		name   string
		method string
		body   string
	}{
		{name: "get", method: http.MethodGet},
		{name: "update", method: http.MethodPut, body: customerE2EUpdateBody},
		{name: "delete", method: http.MethodDelete},
	}

	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			rec := env.call(t, probe.method, "/customers/"+customerID, tokenB, probe.body)

			require.Equal(t, http.StatusNotFound, rec.Code,
				"another tenant's customer must look nonexistent, never forbidden; body: %s", rec.Body.String())
			assert.NotContains(t, rec.Body.String(), customerID)
			assert.NotContains(t, rec.Body.String(), "Road Runner")
		})
	}

	listedByB := env.call(t, http.MethodGet, "/customers", tokenB, "")
	require.Equal(t, http.StatusOK, listedByB.Code)
	assert.Empty(t, decodeCustomers(t, listedByB), "org B's list must not include org A's customer")

	// Org B's failed writes left org A's customer untouched.
	stillThere := env.call(t, http.MethodGet, "/customers/"+customerID, tokenA, "")
	require.Equal(t, http.StatusOK, stillThere.Code, "body: %s", stillThere.Body.String())
	assert.Equal(t, "Road Runner", decodeCustomer(t, stillThere).Name)
}

func TestCustomerWritesRequireAManagerOrAdmin(t *testing.T) {
	env := newCustomerE2EEnv(t)
	organizationID := env.seedOrganization(t)
	managerToken := env.signInNewMember(t, organizationID, orgdomain.RoleManager)
	memberToken := env.signInNewMember(t, organizationID, orgdomain.RoleMember)

	created := env.call(t, http.MethodPost, "/customers", managerToken, customerE2ECreateBody)
	require.Equal(t, http.StatusCreated, created.Code, "body: %s", created.Body.String())
	customerID := decodeCustomer(t, created).ID
	path := "/customers/" + customerID

	writes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create", method: http.MethodPost, path: "/customers", body: customerE2EUpdateBody},
		{name: "update", method: http.MethodPut, path: path, body: customerE2EUpdateBody},
		{name: "delete", method: http.MethodDelete, path: path},
	}

	for _, write := range writes {
		t.Run(write.name+" as a plain member", func(t *testing.T) {
			rec := env.call(t, write.method, write.path, memberToken, write.body)

			assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
		})
	}

	t.Run("a plain member may list customers", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers", memberToken, "")

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Equal(t, []string{customerID}, customerIDs(decodeCustomers(t, rec)),
			"the member's forbidden create must not have added a customer")
	})

	t.Run("a plain member may read a customer", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, path, memberToken, "")

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		customer := decodeCustomer(t, rec)
		assert.Equal(t, "Road Runner", customer.Name, "the member's forbidden update must not have applied")
		assert.Equal(t, "road.runner@example.com", customer.Email)
	})
}

func TestCustomerRoutesRequireAnAccessToken(t *testing.T) {
	env := newCustomerE2EEnv(t)
	token := env.newSignedInTenant(t, orgdomain.RoleManager)

	created := env.call(t, http.MethodPost, "/customers", token, customerE2ECreateBody)
	require.Equal(t, http.StatusCreated, created.Code, "body: %s", created.Body.String())
	path := "/customers/" + decodeCustomer(t, created).ID

	requests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create", method: http.MethodPost, path: "/customers", body: customerE2EUpdateBody},
		{name: "list", method: http.MethodGet, path: "/customers"},
		{name: "get", method: http.MethodGet, path: path},
		{name: "update", method: http.MethodPut, path: path, body: customerE2EUpdateBody},
		{name: "delete", method: http.MethodDelete, path: path},
	}

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			rec := env.call(t, request.method, request.path, "", request.body)

			assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "Road Runner")
		})
	}

	stillThere := env.call(t, http.MethodGet, path, token, "")
	require.Equal(t, http.StatusOK, stillThere.Code, "body: %s", stillThere.Body.String())
	assert.Equal(t, "Road Runner", decodeCustomer(t, stillThere).Name,
		"no unauthenticated request may have changed or deleted the customer")
}
