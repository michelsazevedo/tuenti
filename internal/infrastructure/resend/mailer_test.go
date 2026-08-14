package resend

import (
	"context"
	"errors"
	"strings"
	"testing"

	resendgo "github.com/resend/resend-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testFrom = "no-reply@example.com"
	testTo   = "wile.coyote@example.com"
	testLink = "https://app.example.com/reset-password?token=abc123"
)

type fakeSender struct {
	calls   int
	request *resendgo.SendEmailRequest
	err     error
}

func (f *fakeSender) SendWithContext(_ context.Context, params *resendgo.SendEmailRequest) (*resendgo.SendEmailResponse, error) {
	f.calls++
	f.request = params

	if f.err != nil {
		return nil, f.err
	}

	return &resendgo.SendEmailResponse{Id: "email-id"}, nil
}

func TestSendPasswordResetEmailComposesTheMessage(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).SendPasswordResetEmail(context.Background(), testTo, testLink))

	require.Equal(t, 1, sender.calls, "the mailer must send exactly one email")
	request := sender.request
	require.NotNil(t, request)

	assert.Equal(t, testFrom, request.From, "the configured sender address must be used")
	assert.Equal(t, []string{testTo}, request.To, "the email must go to the requested recipient only")
	assert.Equal(t, "Reset your password", request.Subject)

	assert.Contains(t, request.Html, testLink, "the HTML body must carry the reset link")
	assert.Contains(t, request.Text, testLink, "the text body must carry the reset link")
	assert.Contains(t, request.Html, resetLinkTTLNotice, "the body must state when the link expires")
}

func TestSendPasswordResetEmailWrapsSendFailures(t *testing.T) {
	sentinel := errors.New("resend unavailable")
	sender := &fakeSender{err: sentinel}

	err := newMailer(sender, testFrom).SendPasswordResetEmail(context.Background(), testTo, testLink)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "the underlying SDK error must stay unwrappable")
	assert.Contains(t, err.Error(), "sending password reset email")
	assert.NotContains(t, err.Error(), testLink, "the reset link must never reach an error message")
}

func TestSendPasswordResetEmailRejectsEmptyInput(t *testing.T) {
	tests := []struct {
		name  string
		email string
		link  string
	}{
		{name: "empty recipient", email: "", link: testLink},
		{name: "empty reset link", email: testTo, link: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sender := &fakeSender{}

			err := newMailer(sender, testFrom).SendPasswordResetEmail(context.Background(), test.email, test.link)

			require.Error(t, err)
			assert.Zero(t, sender.calls, "nothing must be sent when the input is incomplete")
		})
	}
}

func TestSendPasswordResetEmailKeepsTheLinkOutOfMetadata(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).SendPasswordResetEmail(context.Background(), testTo, testLink))

	request := sender.request
	assert.NotContains(t, request.Subject, "token", "the subject must not carry the token")

	for _, tag := range request.Tags {
		assert.NotContains(t, tag.Value, "abc123", "tags are metadata and must not carry the token")
	}

	assert.Empty(t, request.Headers, "no custom headers should be set with link material")
}

func TestSendPasswordResetEmailSanitisesTheLinkInHTML(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendPasswordResetEmail(context.Background(), testTo, "javascript:alert(1)"))

	assert.False(t, strings.Contains(sender.request.Html, `href="javascript:alert(1)"`),
		"a javascript: scheme must not survive into the href")
}
