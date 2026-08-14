package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisClient(t *testing.T) *goredis.Client {
	t.Helper()

	addr := os.Getenv("REDIS_HOST")
	if addr == "" {
		addr = "localhost:6379"
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("redis unavailable at %s: %v (start it with `docker-compose up -d redis`)", addr, err)
	}

	t.Cleanup(func() { _ = client.Close() })

	return client
}

func testKeyPrefix(t *testing.T, client *goredis.Client) string {
	t.Helper()

	prefix := "test:ratelimit:" + t.Name()

	t.Cleanup(func() {
		ctx := context.Background()

		keys, err := client.Keys(ctx, prefix+":*").Result()
		if err != nil || len(keys) == 0 {
			return
		}

		_ = client.Del(ctx, keys...).Err()
	})

	return prefix
}

func requestFrom(ip string) *http.Request {
	return requestWithBody(ip, "")
}

func requestWithBody(ip, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/protected", strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	request.RemoteAddr = ip + ":54321"

	return request
}

func call(t *testing.T, limiter echo.MiddlewareFunc, request *http.Request) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(request, rec)

	served := false

	handler := limiter(func(c echo.Context) error {
		served = true

		return c.NoContent(http.StatusOK)
	})

	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec, served
}

func serveReadingBody(t *testing.T, limiter echo.MiddlewareFunc, request *http.Request) string {
	t.Helper()

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(request, rec)

	var seen []byte

	handler := limiter(func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}

		seen = body

		return c.NoContent(http.StatusOK)
	})

	require.NoError(t, handler(c))
	require.Equal(t, http.StatusOK, rec.Code)

	return string(seen)
}

func TestRateLimitAllowsUpToMaxThenRejects(t *testing.T) {
	const (
		limit  = 3
		caller = "203.0.113.10"
	)

	client := newTestRedisClient(t)

	limiter := RateLimit(client, RateLimitConfig{
		Max:       limit,
		Window:    time.Minute,
		KeyPrefix: testKeyPrefix(t, client),
		Extractor: IPKeyExtractor,
	})

	for attempt := 1; attempt <= limit; attempt++ {
		rec, served := call(t, limiter, requestFrom(caller))

		assert.Equal(t, http.StatusOK, rec.Code, "request %d of %d is still inside the limit", attempt, limit)
		assert.True(t, served, "request %d of %d must reach the handler", attempt, limit)
	}

	rec, served := call(t, limiter, requestFrom(caller))

	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "the request past Max must be rejected")
	assert.False(t, served, "a rejected request must never reach the handler")

	t.Run("the rejection says nothing about the limit", func(t *testing.T) {
		rec, _ := call(t, limiter, requestFrom(caller))

		require.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.JSONEq(t, `{"message":"too many requests"}`, rec.Body.String(),
			"the body must not hint at which limit was hit, or how close the caller is to it")
	})
}

func TestRateLimitCounterResetsWhenTheWindowElapses(t *testing.T) {
	const (
		window = 200 * time.Millisecond
		caller = "203.0.113.20"
	)

	client := newTestRedisClient(t)
	prefix := testKeyPrefix(t, client)

	limiter := RateLimit(client, RateLimitConfig{
		Max:       1,
		Window:    window,
		KeyPrefix: prefix,
		Extractor: IPKeyExtractor,
	})

	rec, served := call(t, limiter, requestFrom(caller))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, served)

	rec, _ = call(t, limiter, requestFrom(caller))
	require.Equal(t, http.StatusTooManyRequests, rec.Code, "the caller must be spent for the rest of the window")

	t.Run("the counter is bound to the window", func(t *testing.T) {
		ttl, err := client.PTTL(context.Background(), prefix+":"+caller).Result()
		require.NoError(t, err)

		assert.Positive(t, ttl, "a counter with no ttl would lock the caller out forever")
		assert.LessOrEqual(t, ttl, window, "the window must not outlive the one that was configured")
	})

	time.Sleep(window + 100*time.Millisecond)

	rec, served = call(t, limiter, requestFrom(caller))

	assert.Equal(t, http.StatusOK, rec.Code, "a fresh window must let the caller back in")
	assert.True(t, served)
}

func TestRateLimitCountsEachIPSeparately(t *testing.T) {
	const (
		noisy = "203.0.113.30"
		quiet = "203.0.113.31"
	)

	client := newTestRedisClient(t)

	limiter := RateLimit(client, RateLimitConfig{
		Max:       1,
		Window:    time.Minute,
		KeyPrefix: testKeyPrefix(t, client),
		Extractor: IPKeyExtractor,
	})

	rec, _ := call(t, limiter, requestFrom(noisy))
	require.Equal(t, http.StatusOK, rec.Code)

	rec, _ = call(t, limiter, requestFrom(noisy))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)

	rec, served := call(t, limiter, requestFrom(quiet))

	assert.Equal(t, http.StatusOK, rec.Code, "one caller's burst must not spend another's quota")
	assert.True(t, served)
}

func TestEmailAndIPKeyExtractorCountsEachAccountSeparately(t *testing.T) {
	const caller = "203.0.113.40"

	client := newTestRedisClient(t)

	limiter := RateLimit(client, RateLimitConfig{
		Max:       1,
		Window:    time.Minute,
		KeyPrefix: testKeyPrefix(t, client),
		Extractor: EmailAndIPKeyExtractor,
	})

	rec, _ := call(t, limiter, requestWithBody(caller, `{"email":"wile@example.com"}`))
	require.Equal(t, http.StatusOK, rec.Code)

	rec, _ = call(t, limiter, requestWithBody(caller, `{"email":"wile@example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)

	rec, served := call(t, limiter, requestWithBody(caller, `{"email":"roadrunner@example.com"}`))

	assert.Equal(t, http.StatusOK, rec.Code, "one account's burst must not spend another's quota")
	assert.True(t, served)

	t.Run("a respelled email is the same account", func(t *testing.T) {
		rec, served := call(t, limiter, requestWithBody(caller, `{"email":"  RoadRunner@Example.COM  "}`))

		assert.Equal(t, http.StatusTooManyRequests, rec.Code,
			"case and padding must not mint a fresh bucket, or the limit is one respelling away from useless")
		assert.False(t, served)
	})
}

func TestEmailAndIPKeyExtractorFallsBackToTheIP(t *testing.T) {
	const (
		limit  = 2
		caller = "203.0.113.50"
	)

	client := newTestRedisClient(t)

	limiter := RateLimit(client, RateLimitConfig{
		Max:       limit,
		Window:    time.Minute,
		KeyPrefix: testKeyPrefix(t, client),
		Extractor: EmailAndIPKeyExtractor,
	})

	bodies := []string{"", "not json at all", `{"email":""}`}

	for attempt, body := range bodies {
		rec, served := call(t, limiter, requestWithBody(caller, body))

		if attempt < limit {
			assert.Equal(t, http.StatusOK, rec.Code, "request %d is inside the limit", attempt+1)
			assert.True(t, served)

			continue
		}

		assert.Equal(t, http.StatusTooManyRequests, rec.Code,
			"an unusable body must still count against its sender")
		assert.False(t, served)
	}
}

func TestEmailAndIPKeyExtractorRestoresTheBodyForTheHandler(t *testing.T) {
	const caller = "203.0.113.60"

	client := newTestRedisClient(t)

	limiter := RateLimit(client, RateLimitConfig{
		Max:       100,
		Window:    time.Minute,
		KeyPrefix: testKeyPrefix(t, client),
		Extractor: EmailAndIPKeyExtractor,
	})

	e := echo.New()
	request := requestWithBody(caller, `{"email":"wile@example.com"}`)
	rec := httptest.NewRecorder()
	c := e.NewContext(request, rec)

	var bound struct {
		Email string `json:"email"`
	}

	handler := limiter(func(c echo.Context) error {
		if err := c.Bind(&bound); err != nil {
			return err
		}

		return c.NoContent(http.StatusOK)
	})

	require.NoError(t, handler(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "wile@example.com", bound.Email,
		"the handler must bind the same email the limiter read — a consumed body would 422 every real request")

	t.Run("a body the limiter could not parse still arrives whole", func(t *testing.T) {
		const malformed = `{"email": broken`

		assert.Equal(t, malformed, serveReadingBody(t, limiter, requestWithBody(caller, malformed)),
			"the handler owns the verdict on malformed input, so it must still receive it")
	})

	t.Run("a body past the peek cap still arrives whole", func(t *testing.T) {
		oversized := fmt.Sprintf(`{"email":"wile@example.com","note":"%s"}`,
			strings.Repeat("x", maxPeekedBodySize))

		assert.Equal(t, oversized, serveReadingBody(t, limiter, requestWithBody(caller, oversized)),
			"the bytes past the cap are never read by the limiter, and must reach the handler untouched")
	})
}

func TestRateLimitFailsOpenWhenRedisIsUnreachable(t *testing.T) {
	const caller = "203.0.113.70"

	client := goredis.NewClient(&goredis.Options{
		Addr:        "localhost:1",
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})

	t.Cleanup(func() { _ = client.Close() })

	limiter := RateLimit(client, RateLimitConfig{
		Max:       1,
		Window:    time.Minute,
		KeyPrefix: "test:ratelimit:unreachable",
		Extractor: IPKeyExtractor,
	})

	for attempt := 1; attempt <= 3; attempt++ {
		rec, served := call(t, limiter, requestFrom(caller))

		assert.Equal(t, http.StatusOK, rec.Code,
			"request %d must be served: a redis outage may cost abuse protection, never availability", attempt)
		assert.True(t, served, "request %d must reach the handler", attempt)
	}
}

func TestRateLimitRejectsAConfigThatCannotLimit(t *testing.T) {
	client := newTestRedisClient(t)

	tests := map[string]RateLimitConfig{
		"no max":                 {Max: 0, Window: time.Minute},
		"negative max":           {Max: -1, Window: time.Minute},
		"no window":              {Max: 5, Window: 0},
		"sub-millisecond window": {Max: 5, Window: time.Microsecond},
	}

	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Panics(t, func() { RateLimit(client, cfg) },
				"a limiter that would 429 every request must fail at wiring time, not in production")
		})
	}
}

func TestRateLimitDefaultsTheOptionalConfig(t *testing.T) {
	const caller = "203.0.113.80"

	client := newTestRedisClient(t)

	limiter := RateLimit(client, RateLimitConfig{Max: 1, Window: time.Minute})

	t.Cleanup(func() {
		_ = client.Del(context.Background(), defaultKeyPrefix+":"+caller).Err()
	})

	rec, served := call(t, limiter, requestFrom(caller))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, served)

	rec, _ = call(t, limiter, requestFrom(caller))

	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "the default extractor must still count by IP")

	exists, err := client.Exists(context.Background(), defaultKeyPrefix+":"+caller).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 1, exists, "an unprefixed limiter must land under the default namespace")
}
