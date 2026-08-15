package resend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testInvitationLink = "https://app.example.com/invitations?token=inv456"
	testOrganization   = "Tuenti"
	testRole           = "admin"
)

func TestSendInvitationComposesTheMessage(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendInvitation(context.Background(), testTo, testOrganization, testRole, testInvitationLink))

	require.Equal(t, 1, sender.calls, "the mailer must send exactly one email")
	request := sender.request
	require.NotNil(t, request)

	assert.Equal(t, testFrom, request.From, "the configured sender address must be used")
	assert.Equal(t, []string{testTo}, request.To, "the email must go to the invited recipient only")
	assert.Equal(t, "You've been invited to join Tuenti", request.Subject)

	assert.Contains(t, request.Html, testInvitationLink, "the HTML body must carry the invitation link")
	assert.Contains(t, request.Text, testInvitationLink, "the text body must carry the invitation link")

	assert.Contains(t, request.Html, testRole, "the invitee must learn which role is being offered")
	assert.Contains(t, request.Text, testRole, "the invitee must learn which role is being offered")
	assert.Contains(t, request.Html, testOrganization)
	assert.Contains(t, request.Text, testOrganization)
}

func TestSendInvitationWrapsSendFailures(t *testing.T) {
	sentinel := errors.New("resend unavailable")
	sender := &fakeSender{err: sentinel}

	err := newMailer(sender, testFrom).
		SendInvitation(context.Background(), testTo, testOrganization, testRole, testInvitationLink)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "the underlying SDK error must stay unwrappable")
	assert.Contains(t, err.Error(), "sending invitation email")
	assert.NotContains(t, err.Error(), testInvitationLink, "the invitation link must never reach an error message")
}

func TestSendInvitationRejectsEmptyInput(t *testing.T) {
	tests := []struct {
		name  string
		email string
		link  string
	}{
		{name: "empty recipient", email: "", link: testInvitationLink},
		{name: "empty invitation link", email: testTo, link: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sender := &fakeSender{}

			err := newMailer(sender, testFrom).
				SendInvitation(context.Background(), test.email, testOrganization, testRole, test.link)

			require.Error(t, err)
			assert.Zero(t, sender.calls, "nothing must be sent when the input is incomplete")
		})
	}
}

func TestSendInvitationKeepsTheLinkOutOfMetadata(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendInvitation(context.Background(), testTo, testOrganization, testRole, testInvitationLink))

	request := sender.request
	assert.NotContains(t, request.Subject, "token", "the subject must not carry the token")
	assert.NotContains(t, request.Subject, "inv456", "the subject must not carry the token")

	for _, tag := range request.Tags {
		assert.NotContains(t, tag.Value, "inv456", "tags are metadata and must not carry the token")
	}

	assert.Empty(t, request.Headers, "no custom headers should be set with link material")
}

func TestSendInvitationSanitisesTheLinkInHTML(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendInvitation(context.Background(), testTo, testOrganization, testRole, "javascript:alert(1)"))

	assert.False(t, strings.Contains(sender.request.Html, `href="javascript:alert(1)"`),
		"a javascript: scheme must not survive into the href")
}

func TestSendInvitationEscapesTheOrganizationNameInHTML(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendInvitation(context.Background(), testTo, `<script>alert(1)</script>`, testRole, testInvitationLink))

	assert.NotContains(t, sender.request.Html, "<script>", "an untrusted organization name must not inject markup")
}

func TestSendInvitationKeepsTheSubjectOnASingleLine(t *testing.T) {
	sender := &fakeSender{}

	require.NoError(t, newMailer(sender, testFrom).
		SendInvitation(context.Background(), testTo, "Tuenti\r\nBcc: attacker@example.com", testRole, testInvitationLink))

	subject := sender.request.Subject
	assert.NotContains(t, subject, "\r", "a control character must not survive into the subject header")
	assert.NotContains(t, subject, "\n", "a control character must not survive into the subject header")
}
