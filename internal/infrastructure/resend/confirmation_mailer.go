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

const welcomeConfirmationSubject = "Confirm your email"

var welcomeConfirmationHTML = template.Must(template.New("welcome_confirmation").Parse(
	`<p>Welcome! Please confirm your email address to activate your account.</p>` +
		`<p><a href="{{ .ConfirmationURL }}">Confirm your email</a></p>` +
		`<p>If you did not create this account, you can safely ignore this email.</p>`,
))

func (m *Mailer) SendWelcomeConfirmation(ctx context.Context, toEmail, confirmationURL string) error {
	if toEmail == "" {
		return errors.New("sending confirmation email: empty recipient")
	}
	if confirmationURL == "" {
		return errors.New("sending confirmation email: empty confirmation link")
	}

	html, err := m.renderConfirmationHTML(confirmationURL)
	if err != nil {
		return err
	}

	request := &resendgo.SendEmailRequest{
		From:    m.from,
		To:      []string{toEmail},
		Subject: welcomeConfirmationSubject,
		Html:    html,
		Text: fmt.Sprintf(
			"Welcome! Please confirm your email address to activate your account.\n\n%s\n\n"+
				"If you did not create this account, you can safely ignore this email.",
			confirmationURL,
		),
	}

	if _, err := m.sender.SendWithContext(ctx, request); err != nil {
		logger := observability.Logger(ctx)
		logger.Warn().
			Str("event", "welcome_confirmation_email_send_failed").
			Str("recipient", toEmail).
			Msg("Welcome confirmation email could not be delivered")
		return fmt.Errorf("sending confirmation email: %w", err)
	}
	return nil
}

func (m *Mailer) renderConfirmationHTML(confirmationURL string) (string, error) {
	var buf bytes.Buffer
	data := struct {
		ConfirmationURL string
	}{ConfirmationURL: confirmationURL}
	if err := welcomeConfirmationHTML.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("sending confirmation email: rendering body: %w", err)
	}
	return buf.String(), nil
}

var _ domain.ConfirmationMailer = (*Mailer)(nil)
