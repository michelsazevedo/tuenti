package resend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testConfirmationLink = "https://app.example.com/confirm-email?token=xyz789"

func TestSendWelcomeConfirmationComposesTheMessage(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendWelcomeConfirmation(context.Background(), testTo, testConfirmationLink))

	require.Equal(t, 1, sender.calls, "the mailer must send exactly one email")
	request := sender.request
	require.NotNil(t, request)

	assert.Equal(t, testFrom, request.From, "the configured sender address must be used")
	assert.Equal(t, []string{testTo}, request.To, "the email must go to the requested recipient only")
	assert.Equal(t, "Confirm your email", request.Subject)

	assert.Contains(t, request.Html, testConfirmationLink, "the HTML body must carry the confirmation link")
	assert.Contains(t, request.Text, testConfirmationLink, "the text body must carry the confirmation link")
}

func TestSendWelcomeConfirmationWrapsSendFailures(t *testing.T) {
	sentinel := errors.New("resend unavailable")
	sender := &fakeSender{err: sentinel}

	err := newMailer(sender, testFrom).
		SendWelcomeConfirmation(context.Background(), testTo, testConfirmationLink)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "the underlying SDK error must stay unwrappable")
	assert.Contains(t, err.Error(), "sending confirmation email")
	assert.NotContains(t, err.Error(), testConfirmationLink, "the confirmation link must never reach an error message")
}

func TestSendWelcomeConfirmationRejectsEmptyInput(t *testing.T) {
	tests := []struct {
		name  string
		email string
		link  string
	}{
		{name: "empty recipient", email: "", link: testConfirmationLink},
		{name: "empty confirmation link", email: testTo, link: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sender := &fakeSender{}

			err := newMailer(sender, testFrom).
				SendWelcomeConfirmation(context.Background(), test.email, test.link)

			require.Error(t, err)
			assert.Zero(t, sender.calls, "nothing must be sent when the input is incomplete")
		})
	}
}

func TestSendWelcomeConfirmationKeepsTheLinkOutOfMetadata(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendWelcomeConfirmation(context.Background(), testTo, testConfirmationLink))

	request := sender.request
	assert.NotContains(t, request.Subject, "token", "the subject must not carry the token")

	for _, tag := range request.Tags {
		assert.NotContains(t, tag.Value, "xyz789", "tags are metadata and must not carry the token")
	}

	assert.Empty(t, request.Headers, "no custom headers should be set with link material")
}

func TestSendWelcomeConfirmationSanitisesTheLinkInHTML(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendWelcomeConfirmation(context.Background(), testTo, "javascript:alert(1)"))

	assert.False(t, strings.Contains(sender.request.Html, `href="javascript:alert(1)"`),
		"a javascript: scheme must not survive into the href")
}
