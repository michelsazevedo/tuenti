package main

import (
	"github.com/michelsazevedo/tuenti/internal/http"
	"github.com/michelsazevedo/tuenti/internal/http/logging"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"

	"go.uber.org/fx"
)

func main() {
	app := fx.New(
		fx.WithLogger(logging.NewZerologfx),
		database.Db(),
		observability.Telemetry(),
		http.Http(),
	)

	app.Run()
}
