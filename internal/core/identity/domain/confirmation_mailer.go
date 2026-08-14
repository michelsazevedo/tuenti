package domain

import "context"

type ConfirmationMailer interface {
	SendWelcomeConfirmation(ctx context.Context, toEmail, confirmationURL string) error
}
