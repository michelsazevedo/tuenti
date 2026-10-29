package main

import (
	"github.com/michelsazevedo/tuenti/internal/core/catalog"
	"github.com/michelsazevedo/tuenti/internal/core/customer"
	"github.com/michelsazevedo/tuenti/internal/core/identity"
	"github.com/michelsazevedo/tuenti/internal/core/organization"
	"github.com/michelsazevedo/tuenti/internal/http"
	"github.com/michelsazevedo/tuenti/internal/http/logging"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/kafka"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/redis"

	"go.uber.org/fx"
)

func main() {
	fx.New(appOptions()...).Run()
}

// appOptions is the application's dependency graph. It lives apart from main
// so main_test.go validates exactly what main runs.
func appOptions() []fx.Option {
	return []fx.Option{
		fx.WithLogger(logging.NewZerologfx),
		database.Db(),
		redis.Redis(),
		kafka.Broker(),
		observability.Telemetry(),
		identity.Identity(),
		organization.Organization(),
		catalog.Catalog(),
		customer.Customer(),
		http.Http(),
	}
}
