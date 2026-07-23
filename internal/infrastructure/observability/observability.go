package observability

import (
	"context"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
)

func Telemetry() fx.Option {
	return fx.Module("observability",
		fx.Provide(
			NewTracerProvider,
		),
		fx.Invoke(func(lc fx.Lifecycle, tracer *sdktrace.TracerProvider) {
			otel.SetTracerProvider(tracer)

			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					log.Info().
						Msg("Shutting Down Observability")
					return tracer.Shutdown(ctx)
				},
			})
		}),
	)
}
