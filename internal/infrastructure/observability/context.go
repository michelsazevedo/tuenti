package observability

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

type correlationIDKey struct{}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

func Logger(ctx context.Context) zerolog.Logger {
	logger := log.Logger

	if id := CorrelationID(ctx); id != "" {
		logger = logger.With().Str("correlation_id", id).Logger()
	}

	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		logger = logger.With().
			Str("trace_id", sc.TraceID().String()).
			Str("span_id", sc.SpanID().String()).
			Logger()
	}

	return logger
}
