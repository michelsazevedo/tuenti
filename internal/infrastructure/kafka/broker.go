package kafka

import (
	"context"

	"github.com/rs/zerolog/log"
	"go.uber.org/fx"
)

func Broker() fx.Option {
	return fx.Module(
		"kafka",
		fx.Provide(NewProducer),
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
