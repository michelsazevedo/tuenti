package database

import (
	"context"
	"errors"

	"github.com/golang-migrate/migrate/v4"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/rs/zerolog/log"
	"go.uber.org/fx"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const migrationsPath = "file://./db/migrations"

func Db() fx.Option {
	return fx.Module("db",
		fx.Provide(
			NewPgConn,
			NewUnitOfWork,
		),
		fx.Invoke(
			func(lc fx.Lifecycle, pgConn *PgConn) {
				lc.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						return pgConn.Ping(ctx)
					},
					OnStop: func(ctx context.Context) error {
						log.Info().Msg("Shutting Down Database connection")
						pgConn.Close()
						return nil
					},
				})
			},
			runMigrations,
		),
	)
}

func runMigrations(conf *config.Config) error {
	m, err := migrate.New(migrationsPath, conf.GetDatabaseURL())
	if err != nil {
		return err
	}

	if err = m.Up(); !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	return nil
}
