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

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	goredis "github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/api"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/application"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/persistence"
	identityapi "github.com/michelsazevedo/tuenti/internal/core/identity/api"
	identityapp "github.com/michelsazevedo/tuenti/internal/core/identity/application"
	orgapi "github.com/michelsazevedo/tuenti/internal/core/organization/api"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	orgpersistence "github.com/michelsazevedo/tuenti/internal/core/organization/persistence"
	apphttp "github.com/michelsazevedo/tuenti/internal/http"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const itemTestTimeout = 10 * time.Second

type itemEnv struct {
	server *echo.Echo
	pool   *pgxpool.Pool
	conf   *config.Config
	items  *persistence.ItemRepository
}

type tenant struct {
	organizationID pgtype.UUID
	token          string
}

func newItemEnv(t *testing.T) *itemEnv {
	t.Helper()

	conf := itemTestConfig(t)

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), itemTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	pool := pgConn.Pool()
	items := persistence.NewItemRepository(pool)
	memberships := orgpersistence.NewMembershipRepository(pool)
	uow := database.NewUnitOfWork(pgConn)

	handler := api.NewItemHandler(
		application.NewCreateItem(uow),
		application.NewGetItem(items),
		application.NewListItems(items),
		application.NewUpdateItem(uow),
		application.NewDeleteItem(items),
	)

	client := goredis.NewClient(&goredis.Options{Addr: conf.GetRedisAddr(), Password: conf.Redis.Password})
	t.Cleanup(func() { _ = client.Close() })

	server := echo.New()
	server.HTTPErrorHandler = apphttp.HTTPErrorHandler

	apphttp.RegisterRoutes(
		server,
		client,
		conf,
		memberships,
		apphttp.NewHealthzHandler(),
		identityapi.NewAuthzHandler(nil, nil, nil, nil, nil, nil, nil, nil),
		orgapi.NewOrganizationHandler(nil),
		orgapi.NewInvitationHandler(nil, nil, nil, nil),
		handler,
	)

	return &itemEnv{server: server, pool: pool, conf: conf, items: items}
}

func itemTestConfig(t *testing.T) *config.Config {
	t.Helper()

	setDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setDefaultEnv(t, "REDIS_HOST", "localhost:6379")
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

func (env *itemEnv) context(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), itemTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func (env *itemEnv) newTenant(t *testing.T, role orgdomain.Role) tenant {
	t.Helper()

	ctx := env.context(t)
	suffix := randomSuffix(t)

	var industryID pgtype.UUID
	require.NoError(t, env.pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Catalog Api Industry "+suffix, "catalog_api_industry_"+suffix,
	).Scan(&industryID))

	var organizationID pgtype.UUID
	require.NoError(t, env.pool.QueryRow(ctx,
		`INSERT INTO organizations(name, industry_id, number_of_employees) VALUES($1, $2, $3) RETURNING id`,
		"Acme "+suffix, industryID, 1,
	).Scan(&organizationID))

	var userID pgtype.UUID
	require.NoError(t, env.pool.QueryRow(ctx,
		`INSERT INTO users(name, email, password_digest) VALUES($1, $2, $3) RETURNING id`,
		"Wile E. Coyote "+suffix, "wile."+suffix+"@example.com", "not-a-real-digest",
	).Scan(&userID))

	_, err := env.pool.Exec(ctx,
		`INSERT INTO memberships(organization_id, user_id, role) VALUES($1, $2, $3)`,
		organizationID, userID, role)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), itemTestTimeout)
		defer cancel()

		statements := []struct {
			sql string
			id  pgtype.UUID
		}{
			{`DELETE FROM items WHERE organization_id = $1`, organizationID},
			{`DELETE FROM memberships WHERE organization_id = $1`, organizationID},
			{`DELETE FROM organizations WHERE id = $1`, organizationID},
			{`DELETE FROM users WHERE id = $1`, userID},
			{`DELETE FROM industries WHERE id = $1`, industryID},
		}

		for _, statement := range statements {
			if _, err := env.pool.Exec(cleanupCtx, statement.sql, statement.id); err != nil {
				t.Errorf("cleanup failed for %q: %v", statement.sql, err)
			}
		}
	})

	return tenant{
		organizationID: organizationID,
		token:          env.accessToken(t, userID, organizationID),
	}
}

func (env *itemEnv) accessToken(t *testing.T, userID, organizationID pgtype.UUID) string {
	t.Helper()

	claims := identityapp.AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    identityapp.TokenIssuer(env.conf.Settings.Environment),
			Audience:  jwt.ClaimStrings{identityapp.TokenAudience},
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
		OrganizationID: organizationID.String(),
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString([]byte(env.conf.Settings.Secret))
	require.NoError(t, err)

	return token
}

func (env *itemEnv) call(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
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

func (env *itemEnv) seedItem(t *testing.T, owner tenant, mutate func(*domain.Item)) *domain.Item {
	t.Helper()

	item := &domain.Item{
		OrganizationID:  owner.organizationID,
		Name:            "Acme Anvil " + randomSuffix(t),
		Description:     "Heavy",
		Type:            domain.ItemTypeItem,
		Rate:            decimal.RequireFromString("1999.99"),
		Currency:        "USD",
		IncomeAccount:   "4000",
		TrackInventory:  true,
		QuantityInStock: 10,
	}

	if mutate != nil {
		mutate(item)
	}

	require.NoError(t, env.items.Create(env.context(t), item))

	return item
}

type itemBody struct {
	ID              string `json:"id"`
	OrganizationID  string `json:"organization_id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Type            string `json:"type"`
	Rate            string `json:"rate"`
	Currency        string `json:"currency"`
	IncomeAccount   string `json:"income_account"`
	TrackInventory  bool   `json:"track_inventory"`
	QuantityInStock int    `json:"quantity_in_stock"`
	Taxes           []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		TaxNumber string `json:"tax_number"`
		Rate      string `json:"rate"`
		Enabled   bool   `json:"enabled"`
	} `json:"taxes"`
}

func decodeItem(t *testing.T, rec *httptest.ResponseRecorder) itemBody {
	t.Helper()

	var body itemBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "unreadable response: %s", rec.Body.String())

	return body
}

func randomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func TestCreateItem(t *testing.T) {
	env := newItemEnv(t)
	manager := env.newTenant(t, orgdomain.RoleManager)

	rec := env.call(t, http.MethodPost, "/items", manager.token, `{
		"name": "Acme Anvil",
		"description": "Heavy",
		"type": "ITEM",
		"rate": "1999.99",
		"currency": "USD",
		"income_account": "4000",
		"track_inventory": true,
		"quantity_in_stock": 10,
		"taxes": [{"name":"VAT","tax_number":"VAT-001","rate":"7.25","enabled":true}]
	}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	body := decodeItem(t, rec)
	assert.Equal(t, "Acme Anvil", body.Name)
	assert.Equal(t, "ITEM", body.Type)
	assert.Equal(t, "1999.99", body.Rate, "money must come back as an exact decimal string")
	assert.Equal(t, manager.organizationID.String(), body.OrganizationID,
		"the tenant must come from the token, never from the body")
	assert.True(t, body.TrackInventory)
	assert.Equal(t, 10, body.QuantityInStock)

	require.Len(t, body.Taxes, 1)
	assert.Equal(t, "VAT", body.Taxes[0].Name)
	assert.Equal(t, "7.25", body.Taxes[0].Rate)
	assert.NotEmpty(t, body.Taxes[0].ID, "a persisted tax must be addressable by id on the next update")

	var id pgtype.UUID
	require.NoError(t, id.Scan(body.ID))

	stored, err := env.items.FindByID(env.context(t), manager.organizationID, id)
	require.NoError(t, err, "the response must describe a row that actually exists")
	assert.Equal(t, "1999.99", stored.Rate.String())
}

func TestCreateItemNormalizesAService(t *testing.T) {
	env := newItemEnv(t)
	manager := env.newTenant(t, orgdomain.RoleManager)

	rec := env.call(t, http.MethodPost, "/items", manager.token, `{
		"name": "Rocket Consulting",
		"type": "SERVICE",
		"rate": "150.00",
		"currency": "USD",
		"income_account": "4000",
		"track_inventory": true,
		"quantity_in_stock": 99
	}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	body := decodeItem(t, rec)
	assert.False(t, body.TrackInventory, "a service carries no stock, whatever the client asked for")
	assert.Equal(t, 0, body.QuantityInStock)
	assert.Empty(t, body.Taxes, "an item without taxes must serialise as [], never null")
}

func TestCreateItemRejectsInvalidPayloads(t *testing.T) {
	env := newItemEnv(t)
	manager := env.newTenant(t, orgdomain.RoleManager)

	tests := []struct {
		name string
		body string
		code int
	}{
		{
			name: "missing name",
			body: `{"type":"ITEM","rate":"10.00","currency":"USD","income_account":"4000"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "unknown type",
			body: `{"name":"X","type":"BUNDLE","rate":"10.00","currency":"USD","income_account":"4000"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "missing rate",
			body: `{"name":"X","type":"ITEM","currency":"USD","income_account":"4000"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "rate is not a decimal",
			body: `{"name":"X","type":"ITEM","rate":"free","currency":"USD","income_account":"4000"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "rate is not positive",
			body: `{"name":"X","type":"ITEM","rate":"0","currency":"USD","income_account":"4000"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "tax rate is not a decimal",
			body: `{"name":"X","type":"ITEM","rate":"10.00","currency":"USD","income_account":"4000",
				"taxes":[{"name":"VAT","rate":"seven"}]}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "tax rate is out of range",
			body: `{"name":"X","type":"ITEM","rate":"10.00","currency":"USD","income_account":"4000",
				"taxes":[{"name":"VAT","rate":"101"}]}`,
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
			rec := env.call(t, http.MethodPost, "/items", manager.token, test.body)

			require.Equal(t, test.code, rec.Code, rec.Body.String())
		})
	}
}

func TestItemRoutesRequireAnAuthenticatedManagerOrAdmin(t *testing.T) {
	env := newItemEnv(t)
	manager := env.newTenant(t, orgdomain.RoleManager)
	member := env.newTenant(t, orgdomain.RoleMember)

	item := env.seedItem(t, manager, nil)
	path := "/items/" + item.ID.String()

	writes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create", method: http.MethodPost, path: "/items", body: `{"name":"X","type":"ITEM","rate":"1","currency":"USD","income_account":"4000"}`},
		{name: "update", method: http.MethodPut, path: path, body: `{"name":"X","type":"ITEM","rate":"1","currency":"USD","income_account":"4000"}`},
		{name: "delete", method: http.MethodDelete, path: path},
	}

	for _, write := range writes {
		t.Run(write.name+" without a token", func(t *testing.T) {
			rec := env.call(t, write.method, write.path, "", write.body)

			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})

		t.Run(write.name+" as a plain member", func(t *testing.T) {
			rec := env.call(t, write.method, write.path, member.token, write.body)

			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
	}

	t.Run("reads still require a token", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, env.call(t, http.MethodGet, "/items", "", "").Code)
		assert.Equal(t, http.StatusUnauthorized, env.call(t, http.MethodGet, path, "", "").Code)
	})

	t.Run("a plain member may read the catalog", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, env.call(t, http.MethodGet, "/items", member.token, "").Code)
	})
}

func TestGetItem(t *testing.T) {
	env := newItemEnv(t)
	owner := env.newTenant(t, orgdomain.RoleManager)
	intruder := env.newTenant(t, orgdomain.RoleManager)

	item := env.seedItem(t, owner, func(i *domain.Item) {
		i.Taxes = []domain.ItemTax{{Name: "VAT", Rate: decimal.RequireFromString("7.25"), Enabled: true}}
	})

	t.Run("returns the item to its owner", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items/"+item.ID.String(), owner.token, "")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		body := decodeItem(t, rec)
		assert.Equal(t, item.ID.String(), body.ID)
		assert.Equal(t, "1999.99", body.Rate)
		require.Len(t, body.Taxes, 1)
		assert.Equal(t, "7.25", body.Taxes[0].Rate)
	})

	t.Run("hides an item owned by another organization", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items/"+item.ID.String(), intruder.token, "")

		require.Equal(t, http.StatusNotFound, rec.Code,
			"knowing an item id must not be enough to read another tenant's catalog")
	})

	t.Run("rejects a malformed id", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items/not-a-uuid", owner.token, "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("reports an unknown id as not found", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items/"+randomUUID(t).String(), owner.token, "")

		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestListItems(t *testing.T) {
	env := newItemEnv(t)
	owner := env.newTenant(t, orgdomain.RoleManager)
	intruder := env.newTenant(t, orgdomain.RoleManager)

	suffix := randomSuffix(t)

	anvil := env.seedItem(t, owner, func(i *domain.Item) { i.Name = "Anvil " + suffix })
	consulting := env.seedItem(t, owner, func(i *domain.Item) {
		i.Name = "Rocket Consulting " + suffix
		i.Type = domain.ItemTypeService
		i.TrackInventory = false
		i.QuantityInStock = 0
	})

	names := func(t *testing.T, rec *httptest.ResponseRecorder) []string {
		t.Helper()

		var body []itemBody
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())

		found := make([]string, 0, len(body))
		for _, listed := range body {
			found = append(found, listed.Name)
		}

		return found
	}

	t.Run("returns every item in the caller's organization", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items", owner.token, "")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Subset(t, names(t, rec), []string{anvil.Name, consulting.Name})
	})

	t.Run("filters by type", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items?type=SERVICE", owner.token, "")

		require.Equal(t, http.StatusOK, rec.Code)

		found := names(t, rec)
		assert.Contains(t, found, consulting.Name)
		assert.NotContains(t, found, anvil.Name)
	})

	t.Run("filters by search", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items?search=consulting", owner.token, "")

		require.Equal(t, http.StatusOK, rec.Code)

		found := names(t, rec)
		assert.Contains(t, found, consulting.Name)
		assert.NotContains(t, found, anvil.Name)
	})

	t.Run("rejects an unknown type filter", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items?type=BUNDLE", owner.token, "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("does not leak another organization's catalog", func(t *testing.T) {
		rec := env.call(t, http.MethodGet, "/items", intruder.token, "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()),
			"an empty catalog must serialise as [], never null")
	})
}

func TestUpdateItem(t *testing.T) {
	env := newItemEnv(t)
	owner := env.newTenant(t, orgdomain.RoleManager)
	intruder := env.newTenant(t, orgdomain.RoleManager)

	t.Run("replaces the aggregate and keeps a supplied tax identity", func(t *testing.T) {
		item := env.seedItem(t, owner, func(i *domain.Item) {
			i.Taxes = []domain.ItemTax{{Name: "VAT", Rate: decimal.RequireFromString("7.25"), Enabled: true}}
		})
		keptTaxID := item.Taxes[0].ID

		rec := env.call(t, http.MethodPut, "/items/"+item.ID.String(), owner.token, `{
			"name": "Renamed Anvil",
			"description": "Now with rockets",
			"type": "ITEM",
			"rate": "2500.50",
			"currency": "EUR",
			"income_account": "4010",
			"track_inventory": true,
			"quantity_in_stock": 42,
			"taxes": [{"id":"`+keptTaxID.String()+`","name":"VAT (revised)","rate":"8.00","enabled":false}]
		}`)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		body := decodeItem(t, rec)
		assert.Equal(t, "Renamed Anvil", body.Name)
		assert.Equal(t, "2500.5", body.Rate)
		assert.Equal(t, "EUR", body.Currency)
		assert.Equal(t, 42, body.QuantityInStock)

		require.Len(t, body.Taxes, 1)
		assert.Equal(t, keptTaxID.String(), body.Taxes[0].ID,
			"a tax the client sent back by id must keep that id instead of being recreated")
		assert.Equal(t, "VAT (revised)", body.Taxes[0].Name)

		stored, err := env.items.FindByID(env.context(t), owner.organizationID, item.ID)
		require.NoError(t, err)
		assert.Equal(t, "Renamed Anvil", stored.Name)
		require.Len(t, stored.Taxes, 1)
		assert.Equal(t, keptTaxID, stored.Taxes[0].ID)
	})

	t.Run("hides an item owned by another organization", func(t *testing.T) {
		item := env.seedItem(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/items/"+item.ID.String(), intruder.token, `{
			"name": "Hijacked",
			"type": "ITEM",
			"rate": "1.00",
			"currency": "USD",
			"income_account": "4000"
		}`)

		require.Equal(t, http.StatusNotFound, rec.Code,
			"a cross-tenant update must be indistinguishable from an unknown item")

		stored, err := env.items.FindByID(env.context(t), owner.organizationID, item.ID)
		require.NoError(t, err)
		assert.Equal(t, item.Name, stored.Name, "the rejected update must not have touched the row")
	})

	t.Run("reports an unknown id as not found", func(t *testing.T) {
		rec := env.call(t, http.MethodPut, "/items/"+randomUUID(t).String(), owner.token, `{
			"name": "Ghost", "type": "ITEM", "rate": "1.00", "currency": "USD", "income_account": "4000"
		}`)

		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("rejects an invalid payload", func(t *testing.T) {
		item := env.seedItem(t, owner, nil)

		rec := env.call(t, http.MethodPut, "/items/"+item.ID.String(), owner.token,
			`{"name":"","type":"ITEM","rate":"1.00","currency":"USD","income_account":"4000"}`)

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})
}

func TestDeleteItem(t *testing.T) {
	env := newItemEnv(t)
	owner := env.newTenant(t, orgdomain.RoleManager)
	intruder := env.newTenant(t, orgdomain.RoleManager)

	t.Run("soft-deletes the item and answers no content", func(t *testing.T) {
		item := env.seedItem(t, owner, nil)

		rec := env.call(t, http.MethodDelete, "/items/"+item.ID.String(), owner.token, "")

		require.Equal(t, http.StatusNoContent, rec.Code)
		assert.Empty(t, rec.Body.String())

		assert.Equal(t, http.StatusNotFound,
			env.call(t, http.MethodGet, "/items/"+item.ID.String(), owner.token, "").Code,
			"a deleted item must stop being readable")
	})

	t.Run("hides an item owned by another organization", func(t *testing.T) {
		item := env.seedItem(t, owner, nil)

		rec := env.call(t, http.MethodDelete, "/items/"+item.ID.String(), intruder.token, "")

		require.Equal(t, http.StatusNotFound, rec.Code)

		_, err := env.items.FindByID(env.context(t), owner.organizationID, item.ID)
		assert.NoError(t, err, "the item must still belong to its owner")
	})

	t.Run("is not repeatable", func(t *testing.T) {
		item := env.seedItem(t, owner, nil)

		require.Equal(t, http.StatusNoContent,
			env.call(t, http.MethodDelete, "/items/"+item.ID.String(), owner.token, "").Code)

		assert.Equal(t, http.StatusNotFound,
			env.call(t, http.MethodDelete, "/items/"+item.ID.String(), owner.token, "").Code,
			"deleting an already deleted item must not report success")
	})
}

func randomUUID(t *testing.T) pgtype.UUID {
	t.Helper()

	id := pgtype.UUID{Valid: true}
	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	return id
}
