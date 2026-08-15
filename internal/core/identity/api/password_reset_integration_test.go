package api_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/api"
	"github.com/michelsazevedo/tuenti/internal/core/identity/application"
	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	orgapi "github.com/michelsazevedo/tuenti/internal/core/organization/api"
	orgapp "github.com/michelsazevedo/tuenti/internal/core/organization/application"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	orgrepo "github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	apphttp "github.com/michelsazevedo/tuenti/internal/http"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	resetTestTimeout = 10 * time.Second

	seedPassword = "originalpassword"

	rotatedPassword = "brandnewpassword"

	resetTestClientIP = "203.0.113.7"

	msgResetRequested = `{"message":"if an account exists for this email, a password reset link has been sent"}`
	msgResetCompleted = `{"message":"password has been reset"}`

	msgResetRejected = `{"message":"Unauthorized"}`

	msgTooManyRequests = `{"message":"Too Many Requests"}`
)

type sentEmail struct {
	to   string
	link string
}

type captureMailer struct {
	mu   sync.Mutex
	sent []sentEmail
	err  error
}

func (m *captureMailer) SendPasswordResetEmail(_ context.Context, toEmail, resetLink string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sent = append(m.sent, sentEmail{to: toEmail, link: resetLink})

	return m.err
}

func (m *captureMailer) messages() []sentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]sentEmail(nil), m.sent...)
}

func (e sentEmail) token(t *testing.T) string {
	t.Helper()

	parsed, err := url.Parse(e.link)
	require.NoError(t, err, "the reset link must be a usable URL")

	raw := parsed.Query().Get("token")
	require.NotEmpty(t, raw, "the reset link must carry a token: %s", e.link)

	return raw
}

type resetEnv struct {
	server       *echo.Echo
	pool         *pgxpool.Pool
	mailer       *captureMailer
	users        domain.UserRepository
	tokens       domain.PasswordResetTokenRepository
	refreshStore domain.RefreshTokenStore
}

func newResetEnv(t *testing.T) *resetEnv {
	t.Helper()

	conf := testConfig(t)

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), resetTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	client := goredis.NewClient(&goredis.Options{
		Addr:     conf.GetRedisAddr(),
		Password: conf.Redis.Password,
		DB:       conf.Redis.DB,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("redis unavailable at %s: %v (start it with `docker compose up -d redis`)",
			conf.GetRedisAddr(), err)
	}

	t.Cleanup(func() { _ = client.Close() })

	clearRateLimitCounters(t, client)

	pool := pgConn.Pool()
	users := persistence.NewUserRepository(pool)
	tokens := persistence.NewPasswordResetTokenRepository(pool)
	refreshStore := persistence.NewRefreshTokenStore(client)
	memberships := orgrepo.NewMembershipRepository(pool)
	mailer := &captureMailer{}

	authz := api.NewAuthzHandler(
		nil,
		application.NewSignin(users, memberships, refreshStore, conf),
		nil,
		application.NewRefresh(refreshStore, memberships, conf),
		application.NewRequestPasswordReset(users, tokens, mailer, conf),
		application.NewConfirmPasswordReset(database.NewUnitOfWork(pgConn), tokens, refreshStore),
		nil,
		nil,
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
		orgapi.NewOrganizationHandler(orgapp.NewGetOrganizationByID(orgrepo.NewOrganizationRepository(pool))),
		orgapi.NewInvitationHandler(nil, nil, nil, nil),
	)

	return &resetEnv{
		server:       server,
		pool:         pool,
		mailer:       mailer,
		users:        users,
		tokens:       tokens,
		refreshStore: refreshStore,
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()

	setDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setDefaultEnv(t, "REDIS_HOST", "localhost:6379")
	setDefaultEnv(t, "RESEND_API_KEY", "re_test_key")
	setDefaultEnv(t, "RESEND_FROM_EMAIL", "no-reply@example.com")
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

func clearRateLimitCounters(t *testing.T, client *goredis.Client) {
	t.Helper()

	drop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), resetTestTimeout)
		defer cancel()

		keys, err := client.Keys(ctx, "ratelimit:password-reset*"+resetTestClientIP+"*").Result()
		if err != nil {
			t.Logf("could not list rate limit counters: %v", err)

			return
		}

		if len(keys) == 0 {
			return
		}

		if err := client.Del(ctx, keys...).Err(); err != nil {
			t.Logf("could not clear rate limit counters: %v", err)
		}
	}

	drop()
	t.Cleanup(drop)
}

func (env *resetEnv) post(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderXRealIP, resetTestClientIP)

	rec := httptest.NewRecorder()
	env.server.ServeHTTP(rec, req)

	return rec
}

func (env *resetEnv) requestReset(t *testing.T, email string) *httptest.ResponseRecorder {
	t.Helper()

	return env.post(t, "/auth/password-reset", `{"email":"`+email+`"}`)
}

func (env *resetEnv) confirmReset(t *testing.T, rawToken, newPassword string) *httptest.ResponseRecorder {
	t.Helper()

	return env.post(t, "/auth/password-reset/confirm",
		`{"token":"`+rawToken+`","new_password":"`+newPassword+`"}`)
}

func (env *resetEnv) context(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), resetTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func (env *resetEnv) createResetTestUser(t *testing.T) *domain.User {
	t.Helper()

	ctx := env.context(t)

	digest, err := bcrypt.GenerateFromPassword([]byte(seedPassword), bcrypt.MinCost)
	require.NoError(t, err)

	suffix := randomSuffix(t)
	user := &domain.User{
		Name:           "Wile E. Coyote " + suffix,
		Email:          "wile." + suffix + "@example.com",
		PasswordDigest: string(digest),
	}
	require.NoError(t, env.users.Create(ctx, user))

	now := time.Now().UTC()
	org := &orgdomain.Organization{
		Name:               "Acme " + suffix,
		TrialStartsAt:      now,
		TrialEndsAt:        now.Add(14 * 24 * time.Hour),
		SubscriptionStatus: orgdomain.Trialing,
	}
	require.NoError(t, orgrepo.NewOrganizationRepository(env.pool).Create(ctx, org))

	require.NoError(t, orgrepo.NewMembershipRepository(env.pool).Create(ctx, &orgdomain.Membership{
		OrganizationId: org.Id,
		UserId:         user.Id,
		Role:           orgdomain.RoleManager,
	}))

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), resetTestTimeout)
		defer cancel()

		if _, err := env.pool.Exec(cleanupCtx,
			`DELETE FROM password_reset_tokens WHERE user_id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for the password reset tokens of user %v: %v", user.Id, err)
		}

		if _, err := env.pool.Exec(cleanupCtx, `DELETE FROM memberships WHERE user_id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for the membership of user %v: %v", user.Id, err)
		}

		if _, err := env.pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, user.Id); err != nil {
			t.Errorf("cleanup failed for user %v: %v", user.Id, err)
		}

		if _, err := env.pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id = $1`, org.Id); err != nil {
			t.Errorf("cleanup failed for organization %v: %v", org.Id, err)
		}
	})

	return user
}

func randomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func TestPasswordResetEndToEnd(t *testing.T) {
	env := newResetEnv(t)
	ctx := env.context(t)

	user := env.createResetTestUser(t)

	sessionToken, err := env.refreshStore.Save(ctx, user.Id.String(), 2*time.Minute)
	require.NoError(t, err)

	rec := env.requestReset(t, user.Email)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, msgResetRequested, rec.Body.String())

	messages := env.mailer.messages()
	require.Len(t, messages, 1, "exactly one reset mail must be sent")
	assert.Equal(t, user.Email, messages[0].to)

	rawToken := messages[0].token(t)

	t.Run("the raw token is never persisted", func(t *testing.T) {
		stored, err := env.tokens.FindByDigest(ctx, domain.HashPasswordResetToken(rawToken))
		require.NoError(t, err)

		assert.NotEqual(t, rawToken, stored.TokenDigest,
			"storing the raw token would let a database leak reset every account")
		assert.Nil(t, stored.UsedAt, "the freshly issued token has not been spent yet")
	})

	confirmed := env.confirmReset(t, rawToken, rotatedPassword)
	require.Equal(t, http.StatusOK, confirmed.Code)
	assert.JSONEq(t, msgResetCompleted, confirmed.Body.String())
	assert.NotContains(t, confirmed.Body.String(), rotatedPassword,
		"the new password must never be echoed back")

	t.Run("the password is rotated", func(t *testing.T) {
		updated, err := env.users.FindByEmail(ctx, user.Email)
		require.NoError(t, err)

		assert.NoError(t, bcrypt.CompareHashAndPassword(
			[]byte(updated.PasswordDigest), []byte(rotatedPassword)),
			"the stored digest must verify against the new password")
		assert.Error(t, bcrypt.CompareHashAndPassword(
			[]byte(updated.PasswordDigest), []byte(seedPassword)),
			"the old password must stop working")
	})

	t.Run("the token is burned", func(t *testing.T) {
		spent, err := env.tokens.FindByDigest(ctx, domain.HashPasswordResetToken(rawToken))
		require.NoError(t, err)

		require.NotNil(t, spent.UsedAt, "a redeemed token must be stamped used")
		assert.ErrorIs(t, spent.Validate(time.Now()), domain.ErrPasswordResetTokenUsed)
	})

	t.Run("existing sessions are revoked", func(t *testing.T) {
		_, err := env.refreshStore.Validate(ctx, sessionToken)

		assert.Error(t, err, "a session that survived the reset would keep an attacker signed in")
	})

	t.Run("the token cannot be spent twice", func(t *testing.T) {
		replayed := env.confirmReset(t, rawToken, "yetanotherpassword")

		assert.Equal(t, http.StatusUnauthorized, replayed.Code)
		assert.JSONEq(t, msgResetRejected, replayed.Body.String())
	})
}

func TestRequestPasswordResetDoesNotEnumerateAccounts(t *testing.T) {
	env := newResetEnv(t)

	user := env.createResetTestUser(t)
	unknown := "nobody." + randomSuffix(t) + "@example.com"

	known := env.requestReset(t, user.Email)
	require.Equal(t, http.StatusOK, known.Code)

	stranger := env.requestReset(t, unknown)
	require.Equal(t, http.StatusOK, stranger.Code)

	assert.Equal(t, known.Code, stranger.Code)
	assert.Equal(t, known.Body.String(), stranger.Body.String(),
		"an unknown address must answer byte-identically to a registered one")
	assert.Equal(t, known.Header().Get(echo.HeaderContentType), stranger.Header().Get(echo.HeaderContentType))

	messages := env.mailer.messages()
	require.Len(t, messages, 1, "only the registered address may receive mail")
	assert.Equal(t, user.Email, messages[0].to)
}

func TestConfirmPasswordResetRejectsUnusableTokens(t *testing.T) {
	env := newResetEnv(t)
	ctx := env.context(t)

	user := env.createResetTestUser(t)

	neverIssued, err := domain.GeneratePasswordResetToken()
	require.NoError(t, err)

	expired, err := domain.GeneratePasswordResetToken()
	require.NoError(t, err)
	require.NoError(t, env.tokens.Create(ctx, &domain.PasswordResetToken{
		UserID:      user.Id,
		TokenDigest: domain.HashPasswordResetToken(expired),
		ExpiresAt:   time.Now().Add(-time.Hour),
	}))

	used, err := domain.GeneratePasswordResetToken()
	require.NoError(t, err)
	require.NoError(t, env.tokens.Create(ctx, &domain.PasswordResetToken{
		UserID:      user.Id,
		TokenDigest: domain.HashPasswordResetToken(used),
		ExpiresAt:   time.Now().Add(30 * time.Minute),
	}))
	require.Equal(t, http.StatusOK, env.confirmReset(t, used, rotatedPassword).Code)

	tests := []struct {
		name  string
		token string
	}{
		{name: "never issued", token: neverIssued},
		{name: "expired", token: expired},
		{name: "already used", token: used},
	}

	type response struct {
		code int
		body string
	}

	responses := make(map[string]response, len(tests))

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := env.confirmReset(t, test.token, "adifferentpassword")

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.JSONEq(t, msgResetRejected, rec.Body.String())

			responses[test.name] = response{code: rec.Code, body: rec.Body.String()}
		})
	}

	require.Len(t, responses, len(tests))

	baseline := responses["never issued"]

	for name, got := range responses {
		assert.Equal(t, baseline, got,
			"%s must be indistinguishable from a token that never existed", name)
	}
}

func TestPasswordResetRejectsMalformedInput(t *testing.T) {
	env := newResetEnv(t)

	tests := []struct {
		name string
		path string
		body string
		code int
	}{
		{
			name: "request with no email",
			path: "/auth/password-reset",
			body: `{}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "request with a malformed email",
			path: "/auth/password-reset",
			body: `{"email":"not-an-address"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "request with an unparseable body",
			path: "/auth/password-reset",
			body: `{"email":`,
			code: http.StatusBadRequest,
		},
		{
			name: "confirm with no token",
			path: "/auth/password-reset/confirm",
			body: `{"new_password":"brandnewpassword"}`,
			code: http.StatusUnprocessableEntity,
		},
		{
			name: "confirm with a password under the minimum length",
			path: "/auth/password-reset/confirm",
			body: `{"token":"whatever","new_password":"short"}`,
			code: http.StatusUnprocessableEntity,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := env.post(t, test.path, test.body)

			assert.Equal(t, test.code, rec.Code)
		})
	}

	assert.Empty(t, env.mailer.messages(), "a rejected request must never send mail")
}

func TestPasswordResetEndpointsAreRateLimited(t *testing.T) {
	env := newResetEnv(t)

	user := env.createResetTestUser(t)

	t.Run("requesting a reset", func(t *testing.T) {
		for attempt := 1; attempt <= 5; attempt++ {
			rec := env.requestReset(t, user.Email)
			require.Equal(t, http.StatusOK, rec.Code, "attempt %d is inside the budget", attempt)
		}

		rec := env.requestReset(t, user.Email)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.JSONEq(t, msgTooManyRequests, rec.Body.String())
	})

	t.Run("confirming a reset", func(t *testing.T) {
		for attempt := 1; attempt <= 10; attempt++ {
			rec := env.confirmReset(t, "guess-"+randomSuffix(t), rotatedPassword)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "attempt %d is inside the budget", attempt)
		}

		rec := env.confirmReset(t, "guess-"+randomSuffix(t), rotatedPassword)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	})

	t.Run("other auth routes keep their own budget", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set(echo.HeaderXRealIP, resetTestClientIP)

		rec := httptest.NewRecorder()
		env.server.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
