package kafka

import (
	"context"

	"github.com/rs/zerolog/log"
	"go.uber.org/fx"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

func Kafka() fx.Option {
	return fx.Module(
		"kafka",
		fx.Provide(
			NewProducer,
			fx.Annotate(NewEventPublisher, fx.As(new(identitydomain.ConfirmationEventPublisher))),
			fx.Annotate(NewEventPublisher, fx.As(new(identitydomain.PasswordResetEventPublisher))),
			fx.Annotate(NewEventPublisher, fx.As(new(orgdomain.InvitationEventPublisher))),
		),
		fx.Invoke(
			func(lc fx.Lifecycle, producer *Producer) {
				lc.Append(fx.Hook{
					OnStop: func(ctx context.Context) error {
						log.Info().Msg("Shutting Down Kafka producer")
						return producer.Close()
					},
				})
			},
		),
	)
}
