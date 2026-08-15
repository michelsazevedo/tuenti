package resend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"strings"
	texttemplate "text/template"

	resendgo "github.com/resend/resend-go/v2"

	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const invitationTTLNotice = "7 days"

var invitationSubject = texttemplate.Must(texttemplate.New("invitation_subject").Parse(
	`You've been invited to join {{ .OrganizationName }}`,
))

var invitationHTML = template.Must(template.New("invitation").Parse(
	`<p>You've been invited to join {{ .OrganizationName }} as {{ .Role }}.</p>` +
		`<p><a href="{{ .InvitationURL }}">Accept your invitation</a></p>` +
		`<p>This invitation expires in {{ .TTL }}. If you were not expecting it, you can safely ignore this email.</p>`,
))

type invitationData struct {
	OrganizationName string
	Role             string
	InvitationURL    string
	TTL              string
}

func (m *Mailer) SendInvitation(ctx context.Context, email, organizationName, role, invitationURL string) error {
	if email == "" {
		return errors.New("sending invitation email: empty recipient")
	}

	if invitationURL == "" {
		return errors.New("sending invitation email: empty invitation link")
	}

	data := invitationData{
		OrganizationName: sanitizeHeaderValue(organizationName),
		Role:             sanitizeHeaderValue(role),
		InvitationURL:    invitationURL,
		TTL:              invitationTTLNotice,
	}

	subject, err := m.renderInvitationSubject(data)
	if err != nil {
		return err
	}

	html, err := m.renderInvitationHTML(data)
	if err != nil {
		return err
	}

	request := &resendgo.SendEmailRequest{
		From:    m.from,
		To:      []string{email},
		Subject: subject,
		Html:    html,
		Text: fmt.Sprintf(
			"You've been invited to join %s as %s.\n\n%s\n\n"+
				"This invitation expires in %s. If you were not expecting it, you can safely ignore this email.",
			data.OrganizationName, data.Role, invitationURL, invitationTTLNotice,
		),
	}

	if _, err := m.sender.SendWithContext(ctx, request); err != nil {
		logger := observability.Logger(ctx)
		logger.Warn().
			Str("event", "invitation_email_send_failed").
			Msg("Invitation email could not be delivered")

		return fmt.Errorf("sending invitation email: %w", err)
	}

	return nil
}

func (m *Mailer) renderInvitationSubject(data invitationData) (string, error) {
	var buf bytes.Buffer

	if err := invitationSubject.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("sending invitation email: rendering subject: %w", err)
	}

	return buf.String(), nil
}

func (m *Mailer) renderInvitationHTML(data invitationData) (string, error) {
	var buf bytes.Buffer

	if err := invitationHTML.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("sending invitation email: rendering body: %w", err)
	}

	return buf.String(), nil
}

func sanitizeHeaderValue(value string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}

		return r
	}, value)
}

var _ orgdomain.InvitationMailer = (*Mailer)(nil)
