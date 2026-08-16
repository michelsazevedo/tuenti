package domain

import "context"

type ConfirmationEventPublisher interface {
	PublishEmailConfirmationRequested(ctx context.Context, event EmailConfirmationRequested) error
}
