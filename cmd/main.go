package main

import (
	"github.com/michelsazevedo/tuenti/internal/core/identity"
	"github.com/michelsazevedo/tuenti/internal/http"
	"github.com/michelsazevedo/tuenti/internal/http/logging"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/redis"

	"go.uber.org/fx"
)

func main() {
	app := fx.New(
		fx.WithLogger(logging.NewZerologfx),
		database.Db(),
		redis.Redis(),
		observability.Telemetry(),
		identity.Identity(),
		http.Http(),
	)

	app.Run()
}
