package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/identity/api"
	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

func (env *resetEnv) signin(t *testing.T, email, password string) *httptest.ResponseRecorder {
	t.Helper()

	return env.post(t, "/auth/signin", `{"email":"`+email+`","password":"`+password+`"}`)
}

func (env *resetEnv) refresh(t *testing.T, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()

	return env.post(t, "/auth/refresh", `{"refresh_token":"`+refreshToken+`"}`)
}

func tokenPair(t *testing.T, rec *httptest.ResponseRecorder) api.TokenResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var tokens api.TokenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tokens), "body: %s", rec.Body.String())

	require.NotEmpty(t, tokens.AccessToken, "a 200 with no access token is not a session")
	require.NotEmpty(t, tokens.RefreshToken, "a 200 with no refresh token cannot be renewed")
	assert.Equal(t, "Bearer", tokens.TokenType)
	assert.Positive(t, tokens.ExpiresIn, "the client needs a lifetime to schedule its renewal against")

	return tokens
}

func (env *resetEnv) revokeSessionsOnCleanup(t *testing.T, user *domain.User) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), resetTestTimeout)
		defer cancel()

		if err := env.refreshStore.RevokeAllForUser(ctx, user.Id.String()); err != nil {
			t.Errorf("cleanup failed for the sessions of user %v: %v", user.Id, err)
		}
	})
}

func (env *resetEnv) completeReset(t *testing.T, email, newPassword string) string {
	t.Helper()

	requested := env.requestReset(t, email)
	require.Equal(t, http.StatusOK, requested.Code)

	messages := env.mailer.messages()
	require.NotEmpty(t, messages, "the reset request must have produced a mail to take the token from")

	rawToken := messages[len(messages)-1].token(t)

	confirmed := env.confirmReset(t, rawToken, newPassword)
	require.Equal(t, http.StatusOK, confirmed.Code, "body: %s", confirmed.Body.String())

	return rawToken
}

func TestPasswordResetSwapsWhichCredentialsSigninAccepts(t *testing.T) {
	env := newResetEnv(t)

	user := env.createResetTestUser(t)
	env.revokeSessionsOnCleanup(t, user)

	env.completeReset(t, user.Email, rotatedPassword)

	t.Run("the new password opens a session", func(t *testing.T) {
		tokens := tokenPair(t, env.signin(t, user.Email, rotatedPassword))

		assert.NotEqual(t, tokens.AccessToken, tokens.RefreshToken,
			"the two tokens have different lifetimes and audiences and must not be the same value")
	})

	t.Run("the old password no longer opens one", func(t *testing.T) {
		rec := env.signin(t, user.Email, seedPassword)

		require.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
		assert.NotContains(t, rec.Body.String(), seedPassword,
			"a rejection must not quote the credential back")
	})
}

func TestPasswordResetRevokesSessionsAtTheRefreshEndpoint(t *testing.T) {
	env := newResetEnv(t)

	user := env.createResetTestUser(t)
	env.revokeSessionsOnCleanup(t, user)

	untouched := tokenPair(t, env.signin(t, user.Email, seedPassword))
	renewed := tokenPair(t, env.signin(t, user.Email, seedPassword))

	control := env.refresh(t, renewed.RefreshToken)
	require.Equal(t, http.StatusOK, control.Code,
		"a live pre-reset token must renew, or the assertions after the reset prove nothing")

	rotated := tokenPair(t, control)

	assert.NotEqual(t, renewed.RefreshToken, rotated.RefreshToken,
		"renewal must hand back a new token rather than extend the presented one")

	env.completeReset(t, user.Email, rotatedPassword)

	t.Run("a session untouched by the reset stops renewing", func(t *testing.T) {
		rec := env.refresh(t, untouched.RefreshToken)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("a session that had already been renewed stops renewing too", func(t *testing.T) {
		rec := env.refresh(t, rotated.RefreshToken)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	})

	t.Run("signing in again produces a session that does renew", func(t *testing.T) {
		fresh := tokenPair(t, env.signin(t, user.Email, rotatedPassword))

		tokenPair(t, env.refresh(t, fresh.RefreshToken))
	})
}

func TestASecondResetRequestSupersedesTheFirstToken(t *testing.T) {
	env := newResetEnv(t)

	user := env.createResetTestUser(t)
	env.revokeSessionsOnCleanup(t, user)

	require.Equal(t, http.StatusOK, env.requestReset(t, user.Email).Code)
	require.Equal(t, http.StatusOK, env.requestReset(t, user.Email).Code)

	messages := env.mailer.messages()
	require.Len(t, messages, 2, "each request must produce its own mail")

	superseded := messages[0].token(t)
	current := messages[1].token(t)

	require.NotEqual(t, superseded, current,
		"a second request that reissued the same token would make supersession meaningless")

	t.Run("the first token is refused", func(t *testing.T) {
		rec := env.confirmReset(t, superseded, rotatedPassword)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.JSONEq(t, msgResetRejected, rec.Body.String(),
			"the refusal must read the same as any other dead token, not name supersession as the reason")
	})

	t.Run("the password is untouched by the refusal", func(t *testing.T) {
		tokenPair(t, env.signin(t, user.Email, seedPassword))
	})

	t.Run("the current token still works", func(t *testing.T) {
		rec := env.confirmReset(t, current, rotatedPassword)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.JSONEq(t, msgResetCompleted, rec.Body.String())

		tokenPair(t, env.signin(t, user.Email, rotatedPassword))
	})
}
