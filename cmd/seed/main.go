package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/cmd/seed/seeds"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	bootTimeout = 2 * time.Minute

	seedTimeout = 5 * time.Minute

	shutdownTimeout = 15 * time.Second
)

const (
	exitSuccess = 0
	exitFailure = 1
)

// Seed applies one idempotent set of reference data. Implementations live in cmd/seed/seeds.
type Seed interface {
	Run(ctx context.Context) error
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListening()

	var pgConn *database.PgConn

	app := fx.New(
		fx.NopLogger,
		fx.Provide(config.NewConfig),
		database.Db(),
		fx.Populate(&pgConn),
	)

	bootCtx, cancelBoot := context.WithTimeout(ctx, bootTimeout)
	defer cancelBoot()

	if err := app.Start(bootCtx); err != nil {
		log.Error().Err(err).
			Str("event", "seed_runner_boot_failed").
			Msg("seed runner could not start, nothing was seeded")

		return exitFailure
	}

	exitCode := seed(ctx, registry(pgConn))

	if err := shutdown(ctx, app); err != nil {
		log.Error().Err(err).
			Str("event", "seed_runner_shutdown_failed").
			Msg("seed runner could not release its resources cleanly")

		if exitCode == exitSuccess {
			exitCode = exitFailure
		}
	}

	return exitCode
}

// registry is the ordered list of seeds to apply. Append new seeds here.
func registry(pgConn *database.PgConn) []Seed {
	return []Seed{
		seeds.NewIndustriesSeed(pgConn.Pool()),
	}
}

func seed(ctx context.Context, registered []Seed) int {
	ctx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()

	startedAt := time.Now()

	for _, s := range registered {
		name := fmt.Sprintf("%T", s)

		log.Info().
			Str("event", "seed_started").
			Str("seed", name).
			Msg("seed started")

		seedStartedAt := time.Now()
		err := s.Run(ctx)
		seedElapsed := time.Since(seedStartedAt)

		if err != nil {
			log.Error().Err(err).
				Str("event", "seed_failed").
				Str("seed", name).
				Dur("duration", seedElapsed).
				Msg("seed failed, remaining seeds were skipped")

			return exitFailure
		}

		log.Info().
			Str("event", "seed_succeeded").
			Str("seed", name).
			Dur("duration", seedElapsed).
			Msg("seed completed successfully")
	}

	log.Info().
		Str("event", "seed_runner_succeeded").
		Int("seeds", len(registered)).
		Dur("duration", time.Since(startedAt)).
		Msg("all seeds applied successfully")

	return exitSuccess
}

func shutdown(ctx context.Context, app *fx.App) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	return app.Stop(shutdownCtx)
}
