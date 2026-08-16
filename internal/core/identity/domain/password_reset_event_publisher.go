package domain

import "context"

type PasswordResetEventPublisher interface {
	PublishPasswordResetRequested(ctx context.Context, event PasswordResetRequested) error
}
