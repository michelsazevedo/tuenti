package api_test

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/customer/api"
	"github.com/michelsazevedo/tuenti/internal/core/customer/application"
	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/core/customer/persistence"
	apphttp "github.com/michelsazevedo/tuenti/internal/http"
	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	customerTestTimeout = 10 * time.Second

	// testOrganizationHeader carries the tenant the stand-in auth middleware
	// puts on the request context. Route wiring (and RequireAuth) belongs to
	// internal/http/router.go and is exercised end to end once US-006 lands.
	testOrganizationHeader = "X-Test-Organization-Id"

	// unauthenticatedPrefix mounts the same handler with no identity at all,
	// to prove it fails closed when RequireAuth was not wired in front of it.
	unauthenticatedPrefix = "/unauthenticated"
)

type customerEnv struct {
	server    *echo.Echo
	pool      *pgxpool.Pool
	customers *persistence.CustomerRepository
}

func newCustomerEnv(t *testing.T) *customerEnv {
	t.Helper()

	conf := customerTestConfig(t)

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	pool := pgConn.Pool()
	customers := persistence.NewCustomerRepository(pool)

	handler := api.NewCustomerHandler(
		application.NewCreateCustomer(customers),
		application.NewGetCustomer(customers),
		application.NewListCustomers(customers),
		application.NewUpdateCustomer(customers),
		application.NewDeleteCustomer(customers),
	)

	server := echo.New()
	server.HTTPErrorHandler = apphttp.HTTPErrorHandler

	mountCustomerRoutes(server.Group("/customers", injectTestIdentity), handler)
	mountCustomerRoutes(server.Group(unauthenticatedPrefix+"/customers"), handler)

	return &customerEnv{server: server, pool: pool, customers: customers}
}

func mountCustomerRoutes(group *echo.Group, handler api.CustomerHandler) {
	group.POST("", handler.Create)
	group.GET("", handler.List)
	group.GET("/:id", handler.Get)
	group.PUT("/:id", handler.Update)
	group.DELETE("/:id", handler.Delete)
}

func injectTestIdentity(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		var identity m.Identity
		if err := identity.CurrentOrganizationID.Scan(c.Request().Header.Get(testOrganizationHeader)); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}

		c.SetRequest(c.Request().WithContext(m.WithIdentity(c.Request().Context(), identity)))

		return next(c)
	}
}

func customerTestConfig(t *testing.T) *config.Config {
	t.Helper()

	setDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	return conf
}

func setDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func (env *customerEnv) context(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func (env *customerEnv) newOrganization(t *testing.T) pgtype.UUID {
	t.Helper()

	ctx := env.context(t)
	suffix := randomSuffix(t)

	var industryID pgtype.UUID
	require.NoError(t, env.pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Customer Api Industry "+suffix, "customer_api_industry_"+suffix,
	).Scan(&industryID))

	var organizationID pgtype.UUID
	require.NoError(t, env.pool.QueryRow(ctx,
		`INSERT INTO organizations(name, industry_id, number_of_employees) VALUES($1, $2, $3) RETURNING id`,
		"Acme "+suffix, industryID, 1,
	).Scan(&organizationID))

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), customerTestTimeout)
		defer cancel()

		statements := []struct {
			sql string
			id  pgtype.UUID
		}{
			{`DELETE FROM customers WHERE organization_id = $1`, organizationID},
			{`DELETE FROM organizations WHERE id = $1`, organizationID},
			{`DELETE FROM industries WHERE id = $1`, industryID},
		}

		for _, statement := range statements {
			if _, err := env.pool.Exec(cleanupCtx, statement.sql, statement.id); err != nil {
				t.Errorf("cleanup failed for %q: %v", statement.sql, err)
			}
		}
	})

	return organizationID
}

func (env *customerEnv) call(t *testing.T, method, path string, organizationID pgtype.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	if organizationID.Valid {
		request.Header.Set(testOrganizationHeader, organizationID.String())
	}

	rec := httptest.NewRecorder()
	env.server.ServeHTTP(rec, request)

	return rec
}

func (env *customerEnv) seedCustomer(t *testing.T, organizationID pgtype.UUID, mutate func(*domain.Customer)) *domain.Customer {
	t.Helper()

	suffix := randomSuffix(t)

	customer := &domain.Customer{
		OrganizationID: organizationID,
		Name:           "Wile E. Coyote " + suffix,
		CompanyName:    "Acme Corporation",
		Email:          "wile." + suffix + "@acme.com",
		PhoneNumber:    "+1 555 0100",
		Address:        &domain.Address{Street: "1 Desert Rd", City: "Phoenix", State: "AZ", Country: "US"},
	}

	if mutate != nil {
		mutate(customer)
	}

	require.NoError(t, env.customers.Create(env.context(t), customer))

	return customer
}

type customerBody struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	CompanyName    string `json:"company_name"`
	Email          string `json:"email"`
	PhoneNumber    string `json:"phone_number"`
	Address        *struct {
		Street     string `json:"street"`
		Complement string `json:"complement"`
		City       string `json:"city"`
		State      string `json:"state"`
		Country    string `json:"country"`
	} `json:"address"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func decodeCustomer(t *testing.T, rec *httptest.ResponseRecorder) customerBody {
	t.Helper()

	var body customerBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "unreadable response: %s", rec.Body.String())

	return body
}

func customerPayload(email, country string) string {
	return `{
		"name": "Road Runner",
		"company_name": "Acme Corporation",
		"email": "` + email + `",
		"phone_number": "+1 555 0199",
		"address": {"street": "2 Canyon Way", "complement": "Rock 3", "city": "Tucson", "state": "AZ", "country": "` + country + `"}
	}`
}

func randomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func randomUUID(t *testing.T) pgtype.UUID {
	t.Helper()

	id := pgtype.UUID{Valid: true}
	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	return id
}

func TestCreateCustomer(t *testing.T) {
	env := newCustomerEnv(t)
	organizationID := env.newOrganization(t)

	email := "road." + randomSuffix(t) + "@acme.com"

	rec := env.call(t, http.MethodPost, "/customers", organizationID, customerPayload(email, "us"))

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	body := decodeCustomer(t, rec)
	assert.Equal(t, "Road Runner", body.Name)
	assert.Equal(t, "Acme Corporation", body.CompanyName)
	assert.Equal(t, email, body.Email)
	assert.Equal(t, "+1 555 0199", body.PhoneNumber)
	assert.Equal(t, organizationID.String(), body.OrganizationID,
		"the tenant must come from the authenticated identity, never from the body")
	assert.False(t, body.CreatedAt.IsZero())
	assert.False(t, body.UpdatedAt.IsZero())

	require.NotNil(t, body.Address)
	assert.Equal(t, "Rock 3", body.Address.Complement)
	assert.Equal(t, "US", body.Address.Country, "the country must come back normalized")

	assert.NotContains(t, rec.Body.String(), "deleted_at")

	var id pgtype.UUID
	require.NoError(t, id.Scan(body.ID))

	stored, err := env.customers.FindByID(env.context(t), organizationID, id)
	require.NoError(t, err, "the response must describe a row that actually exists")
	assert.Equal(t, email, stored.Email)
}

func TestCreateCustomerWithoutAddress(t *testing.T) {
	env := newCustomerEnv(t)
	organizationID := env.newOrganization(t)

	rec := env.call(t, http.MethodPost, "/customers", organizationID, `{
		"name": "Road Runner",
		"company_name": "Acme Corporation",
		"email": "road.`+randomSuffix(t)+`@acme.com",
		"phone_number": "+1 555 0199"
	}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Nil(t, decodeCustomer(t, rec).Address)
	assert.Contains(t, rec.Body.String(), `"address":null`,
		"a customer without an address must keep a stable response shape")
}

func TestCreateCustomerRejectsInvalidPayloads(t *testing.T) {
	env := newCustomerEnv(t)
	organizationID := env.newOrganization(t)

	taken := env.seedCustomer(t, organizationID, nil)

	tests := []struct {
		name string
		body string
		code int
	}{
		{
			name: "missing name",
			body: `{"company_name":"Acme","email":"a@acme.com","phone_number":"1"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "missing company name",
			body: `{"name":"A","email":"a@acme.com","phone_number":"1"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "missing email",
			body: `{"name":"A","company_name":"Acme","phone_number":"1"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "malformed email",
			body: `{"name":"A","company_name":"Acme","email":"not-an-email","phone_number":"1"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "missing phone number",
			body: `{"name":"A","company_name":"Acme","email":"a@acme.com"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "blank name passes binding but not the domain",
			body: `{"name":"   ","company_name":"Acme","email":"a@acme.com","phone_number":"1"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "unknown country code",
			body: customerPayload("a@acme.com", "XX"),
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "alpha-3 country code",
			body: customerPayload("a@acme.com", "USA"),
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "email already used in the organization",
			body: customerPayload(strings.ToUpper(taken.Email), "US"),
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "unreadable body",
			body: `{"name":`,
			code: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := env.call(t, http.MethodPost, "/customers", organizationID, test.body)

			require.Equal(t, test.code, rec.Code, rec.Body.String())
		})
	}

	listed, err := env.customers.List(env.context(t), domain.ListCustomersFilter{OrganizationID: organizationID})
	require.NoError(t, err)
	assert.Len(t, listed, 1, "a rejected request must not persist anything")
}

func TestCreateCustomerAllowsAnEmailUsedByAnotherOrganization(t *testing.T) {
	env := newCustomerEnv(t)
	owner := env.newOrganization(t)
	other := env.newOrganization(t)

	taken := env.seedCustomer(t, owner, nil)

	rec := env.call(t, http.MethodPost, "/customers", other, customerPayload(taken.Email, "US"))

	require.Equal(t, http.StatusCreated, rec.Code, "email uniqueness is scoped to one organization")
}

func TestCustomerHandlerFailsClosedWithoutAnIdentity(t *testing.T) {
	env := newCustomerEnv(t)
	organizationID := env.newOrganization(t)

	customer := env.seedCustomer(t, organizationID, nil)
	path := unauthenticatedPrefix + "/customers/" + customer.ID.String()

	requests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create", method: http.MethodPost, path: unauthenticatedPrefix + "/customers", body: customerPayload("x@acme.com", "US")},
		{name: "list", method: http.MethodGet, path: unauthenticatedPrefix + "/customers"},
		{name: "get", method: http.MethodGet, path: path},
		{name: "update", method: http.MethodPut, path: path, body: customerPayload("x@acme.com", "US")},
		{name: "delete", method: http.MethodDelete, path: path},
	}

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			rec := env.call(t, request.method, request.path, organizationID, request.body)

			require.Equal(t, http.StatusInternalServerError, rec.Code,
				"a handler mounted without RequireAuth must refuse to guess a tenant")
		})
	}

	stored, err := env.customers.FindByID(env.context(t), organizationID, customer.ID)
	require.NoError(t, err, "no unauthenticated request may have deleted the customer")
	assert.Equal(t, customer.Name, stored.Name, "no unauthenticated request may have updated the customer")
}

func TestGetCustomer(t *testing.T) {
	env := newCustomerEnv(t)
	owner := env.newOrganization(t)
	intruder := env.newOrganization(t)

	customer := env.seedCustomer(t, owner, nil)

	t.Run("returns the customer to its owner", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers/"+customer.ID.String(), owner, "")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		body := decodeCustomer(t, rec)
		assert.Equal(t, customer.ID.String(), body.ID)
		assert.Equal(t, customer.Email, body.Email)
		require.NotNil(t, body.Address)
		assert.Equal(t, "US", body.Address.Country)
	})

	t.Run("hides a customer owned by another organization", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers/"+customer.ID.String(), intruder, "")

		require.Equal(t, http.StatusNotFound, rec.Code,
			"knowing a customer id must not be enough to read another tenant's customer")
	})

	t.Run("rejects a malformed id", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers/not-a-uuid", owner, "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("reports an unknown id as not found", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers/"+randomUUID(t).String(), owner, "")

		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestListCustomers(t *testing.T) {
	env := newCustomerEnv(t)
	owner := env.newOrganization(t)
	intruder := env.newOrganization(t)

	suffix := randomSuffix(t)

	coyote := env.seedCustomer(t, owner, func(c *domain.Customer) { c.Name = "Wile Coyote " + suffix })
	runner := env.seedCustomer(t, owner, func(c *domain.Customer) {
		c.Name = "Road Runner " + suffix
		c.CompanyName = "Beep Beep Ltd"
		c.Address = nil
	})

	names := func(t *testing.T, rec *httptest.ResponseRecorder) []string {
		t.Helper()

		var body []customerBody
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())

		found := make([]string, 0, len(body))
		for _, listed := range body {
			found = append(found, listed.Name)
		}

		return found
	}

	t.Run("returns every customer in the caller's organization", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers", owner, "")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.ElementsMatch(t, []string{coyote.Name, runner.Name}, names(t, rec))
	})

	t.Run("filters by a trimmed, case-insensitive search", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers?search=%20%20beep%20beep%20", owner, "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, []string{runner.Name}, names(t, rec))
	})

	t.Run("rejects a search term carrying a NUL byte", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers?search=jane%00doe", owner, "")

		require.Equal(t, http.StatusBadRequest, rec.Code,
			"Postgres refuses NUL in text, so it must be rejected before it reaches the query; body: %s", rec.Body.String())
	})

	t.Run("does not leak another organization's customers", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/customers", intruder, "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()),
			"an empty customer list must serialise as [], never null")
	})
}

func TestUpdateCustomer(t *testing.T) {
	env := newCustomerEnv(t)
	owner := env.newOrganization(t)
	intruder := env.newOrganization(t)

	t.Run("replaces the customer", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)
		email := "renamed." + randomSuffix(t) + "@acme.com"

		rec := env.call(t, http.MethodPut, "/customers/"+customer.ID.String(), owner, customerPayload(email, "br"))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		body := decodeCustomer(t, rec)
		assert.Equal(t, customer.ID.String(), body.ID, "the path id must win over anything in the body")
		assert.Equal(t, "Road Runner", body.Name)
		assert.Equal(t, email, body.Email)
		require.NotNil(t, body.Address)
		assert.Equal(t, "BR", body.Address.Country)

		stored, err := env.customers.FindByID(env.context(t), owner, customer.ID)
		require.NoError(t, err)
		assert.Equal(t, "Road Runner", stored.Name)
		assert.Equal(t, email, stored.Email)
	})

	t.Run("can drop the address", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/customers/"+customer.ID.String(), owner, `{
			"name": "No Fixed Abode", "company_name": "Acme", "email": "`+customer.Email+`", "phone_number": "1"
		}`)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Nil(t, decodeCustomer(t, rec).Address)
	})

	t.Run("hides a customer owned by another organization", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/customers/"+customer.ID.String(), intruder,
			customerPayload("hijack."+randomSuffix(t)+"@acme.com", "US"))

		require.Equal(t, http.StatusNotFound, rec.Code,
			"a cross-tenant update must be indistinguishable from an unknown customer")

		stored, err := env.customers.FindByID(env.context(t), owner, customer.ID)
		require.NoError(t, err)
		assert.Equal(t, customer.Name, stored.Name, "the rejected update must not have touched the row")
	})

	t.Run("reports an unknown id as not found", func(t *testing.T) {
		rec := env.call(t, http.MethodPut, "/customers/"+randomUUID(t).String(), owner,
			customerPayload("ghost."+randomSuffix(t)+"@acme.com", "US"))

		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("rejects a malformed id", func(t *testing.T) {
		rec := env.call(t, http.MethodPut, "/customers/not-a-uuid", owner, customerPayload("a@acme.com", "US"))

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("rejects a missing required field", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/customers/"+customer.ID.String(), owner,
			`{"name":"","company_name":"Acme","email":"a@acme.com","phone_number":"1"}`)

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("rejects an invalid country", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/customers/"+customer.ID.String(), owner,
			customerPayload(customer.Email, "Brazil"))

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("rejects an email taken by another customer", func(t *testing.T) {
		taken := env.seedCustomer(t, owner, nil)
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/customers/"+customer.ID.String(), owner,
			customerPayload(taken.Email, "US"))

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

		stored, err := env.customers.FindByID(env.context(t), owner, customer.ID)
		require.NoError(t, err)
		assert.Equal(t, customer.Email, stored.Email, "the rejected update must not have touched the row")
	})

	t.Run("rejects an unreadable body", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/customers/"+customer.ID.String(), owner, `{"name":`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestDeleteCustomer(t *testing.T) {
	env := newCustomerEnv(t)
	owner := env.newOrganization(t)
	intruder := env.newOrganization(t)

	t.Run("soft-deletes the customer and answers no content", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodDelete, "/customers/"+customer.ID.String(), owner, "")

		require.Equal(t, http.StatusNoContent, rec.Code)
		assert.Empty(t, rec.Body.String())

		assert.Equal(t, http.StatusNotFound,
			env.call(t, http.MethodGet, "/customers/"+customer.ID.String(), owner, "").Code,
			"a deleted customer must stop being readable")

		var deletedAt *time.Time
		require.NoError(t, env.pool.QueryRow(env.context(t),
			`SELECT deleted_at FROM customers WHERE id = $1`, customer.ID).Scan(&deletedAt))
		assert.NotNil(t, deletedAt, "delete must be soft: the row stays, flagged")
	})

	t.Run("hides a customer owned by another organization", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		rec := env.call(t, http.MethodDelete, "/customers/"+customer.ID.String(), intruder, "")

		require.Equal(t, http.StatusNotFound, rec.Code)

		_, err := env.customers.FindByID(env.context(t), owner, customer.ID)
		assert.NoError(t, err, "the customer must still belong to its owner")
	})

	t.Run("is not repeatable", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		require.Equal(t, http.StatusNoContent,
			env.call(t, http.MethodDelete, "/customers/"+customer.ID.String(), owner, "").Code)

		assert.Equal(t, http.StatusNotFound,
			env.call(t, http.MethodDelete, "/customers/"+customer.ID.String(), owner, "").Code,
			"deleting an already deleted customer must not report success")
	})

	t.Run("rejects a malformed id", func(t *testing.T) {
		rec := env.call(t, http.MethodDelete, "/customers/not-a-uuid", owner, "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("frees the email for reuse", func(t *testing.T) {
		customer := env.seedCustomer(t, owner, nil)

		require.Equal(t, http.StatusNoContent,
			env.call(t, http.MethodDelete, "/customers/"+customer.ID.String(), owner, "").Code)

		rec := env.call(t, http.MethodPost, "/customers", owner, customerPayload(customer.Email, "US"))

		require.Equal(t, http.StatusCreated, rec.Code, "uniqueness only applies among non-deleted customers")
	})
}

// customerPayloadWith renders a valid create/update body with overrides
// applied; an "address."-prefixed key overrides an address field.
func customerPayloadWith(t *testing.T, overrides map[string]string) string {
	t.Helper()

	address := map[string]string{"street": "2 Canyon Way", "complement": "Rock 3", "city": "Tucson", "state": "AZ", "country": "US"}
	payload := map[string]any{
		"name":         "Road Runner",
		"company_name": "Acme Corporation",
		"email":        "road." + randomSuffix(t) + "@acme.com",
		"phone_number": "+1 555 0199",
		"address":      address,
	}

	for key, value := range overrides {
		if field, ok := strings.CutPrefix(key, "address."); ok {
			address[field] = value
		} else {
			payload[key] = value
		}
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	return string(body)
}

// longEmail is a syntactically valid address longer than the 255-character
// email column, so only the length limit can reject it.
func longEmail() string {
	label := strings.Repeat("b", 63)

	return "a@" + label + "." + label + "." + label + "." + label + ".com"
}

func TestCustomerWritesRejectOversizedOrNULFieldsAsUnprocessable(t *testing.T) {
	env := newCustomerEnv(t)
	owner := env.newOrganization(t)

	require.Greater(t, len(longEmail()), 255)

	tests := []struct {
		name      string
		overrides map[string]string
	}{
		{name: "an overlong name", overrides: map[string]string{"name": strings.Repeat("n", 256)}},
		{name: "an overlong company name", overrides: map[string]string{"company_name": strings.Repeat("c", 256)}},
		{name: "an overlong email", overrides: map[string]string{"email": longEmail()}},
		{name: "an overlong phone number", overrides: map[string]string{"phone_number": strings.Repeat("1", 51)}},
		{name: "an overlong street", overrides: map[string]string{"address.street": strings.Repeat("s", 256)}},
		{name: "an overlong complement", overrides: map[string]string{"address.complement": strings.Repeat("c", 256)}},
		{name: "an overlong city", overrides: map[string]string{"address.city": strings.Repeat("c", 256)}},
		{name: "an overlong state", overrides: map[string]string{"address.state": strings.Repeat("s", 256)}},
		{name: "a NUL byte in the name", overrides: map[string]string{"name": "Road\x00Runner"}},
		{name: "a NUL byte in the company name", overrides: map[string]string{"company_name": "Acme\x00"}},
		{name: "a NUL byte in the phone number", overrides: map[string]string{"phone_number": "555\x000100"}},
		{name: "a NUL byte in the street", overrides: map[string]string{"address.street": "1\x00 Main St"}},
		{name: "a NUL byte in the city", overrides: map[string]string{"address.city": "Tuc\x00son"}},
	}

	existing := env.seedCustomer(t, owner, nil)

	for _, test := range tests {
		t.Run("create with "+test.name, func(t *testing.T) {
			rec := env.call(t, http.MethodPost, "/customers", owner, customerPayloadWith(t, test.overrides))

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		})

		t.Run("update with "+test.name, func(t *testing.T) {
			rec := env.call(t, http.MethodPut, "/customers/"+existing.ID.String(), owner, customerPayloadWith(t, test.overrides))

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		})
	}

	listed, err := env.customers.List(env.context(t), domain.ListCustomersFilter{OrganizationID: owner})
	require.NoError(t, err)
	require.Len(t, listed, 1, "a rejected create must not persist anything")
	assert.Equal(t, existing.Name, listed[0].Name, "a rejected update must not have touched the row")

	t.Run("fields exactly at the column limits are accepted", func(t *testing.T) {
		rec := env.call(t, http.MethodPost, "/customers", owner, customerPayloadWith(t, map[string]string{
			"name":           strings.Repeat("n", 255),
			"company_name":   strings.Repeat("c", 255),
			"phone_number":   strings.Repeat("1", 50),
			"address.street": strings.Repeat("s", 255),
		}))

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	})
}
