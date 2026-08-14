package resend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"

	resendgo "github.com/resend/resend-go/v2"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

const resetLinkTTLNotice = "30 minutes"

const passwordResetSubject = "Reset your password"

type emailSender interface {
	SendWithContext(ctx context.Context, params *resendgo.SendEmailRequest) (*resendgo.SendEmailResponse, error)
}

var passwordResetHTML = template.Must(template.New("password_reset").Parse(
	`<p>We received a request to reset your password.</p>` +
		`<p><a href="{{ .ResetLink }}">Reset your password</a></p>` +
		`<p>This link expires in {{ .TTL }}. If you did not request a password reset, you can safely ignore this email.</p>`,
))

type Mailer struct {
	sender emailSender
	from   string
}

func NewMailer(client *resendgo.Client, fromEmail string) *Mailer {
	return newMailer(client.Emails, fromEmail)
}

func newMailer(sender emailSender, fromEmail string) *Mailer {
	return &Mailer{sender: sender, from: fromEmail}
}

func (m *Mailer) SendPasswordResetEmail(ctx context.Context, toEmail, resetLink string) error {
	if toEmail == "" {
		return errors.New("sending password reset email: empty recipient")
	}

	if resetLink == "" {
		return errors.New("sending password reset email: empty reset link")
	}

	html, err := m.renderHTML(resetLink)
	if err != nil {
		return err
	}

	request := &resendgo.SendEmailRequest{
		From:    m.from,
		To:      []string{toEmail},
		Subject: passwordResetSubject,
		Html:    html,
		Text: fmt.Sprintf(
			"We received a request to reset your password.\n\n%s\n\n"+
				"This link expires in %s. If you did not request a password reset, you can safely ignore this email.",
			resetLink, resetLinkTTLNotice,
		),
	}

	if _, err := m.sender.SendWithContext(ctx, request); err != nil {
		logger := observability.Logger(ctx)
		logger.Warn().
			Str("event", "password_reset_email_send_failed").
			Str("recipient", toEmail).
			Msg("Password reset email could not be delivered")

		return fmt.Errorf("sending password reset email: %w", err)
	}

	return nil
}

func (m *Mailer) renderHTML(resetLink string) (string, error) {
	var buf bytes.Buffer

	data := struct {
		ResetLink string
		TTL       string
	}{ResetLink: resetLink, TTL: resetLinkTTLNotice}

	if err := passwordResetHTML.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("sending password reset email: rendering body: %w", err)
	}

	return buf.String(), nil
}

var _ domain.PasswordResetMailer = (*Mailer)(nil)
