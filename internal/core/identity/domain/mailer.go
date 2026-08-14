package domain

import "context"

type PasswordResetMailer interface {
	SendPasswordResetEmail(ctx context.Context, toEmail, resetLink string) error
}
