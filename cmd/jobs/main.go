package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/core/organization"
	"github.com/michelsazevedo/tuenti/internal/core/organization/application"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	bootTimeout = 2 * time.Minute

	sweepTimeout = 10 * time.Minute

	shutdownTimeout = 15 * time.Second
)

const (
	exitSuccess = 0
	exitFailure = 1
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stopListening := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListening()

	var suspendExpiredTrials application.SuspendExpiredTrialsUseCase

	app := fx.New(
		fx.NopLogger,
		fx.Provide(config.NewConfig),
		database.Db(),
		organization.Organization(),
		fx.Populate(&suspendExpiredTrials),
	)

	bootCtx, cancelBoot := context.WithTimeout(ctx, bootTimeout)
	defer cancelBoot()

	if err := app.Start(bootCtx); err != nil {
		log.Error().Err(err).
			Str("event", "trial_sweep_job_boot_failed").
			Msg("trial sweep job could not start, nothing was suspended")

		return exitFailure
	}

	exitCode := sweep(ctx, suspendExpiredTrials)

	if err := shutdown(ctx, app); err != nil {
		log.Error().Err(err).
			Str("event", "trial_sweep_job_shutdown_failed").
			Msg("trial sweep job could not release its resources cleanly")

		if exitCode == exitSuccess {
			exitCode = exitFailure
		}
	}

	return exitCode
}

func sweep(ctx context.Context, useCase application.SuspendExpiredTrialsUseCase) int {
	ctx, cancel := context.WithTimeout(ctx, sweepTimeout)
	defer cancel()

	startedAt := time.Now()
	suspended, err := useCase.Execute(ctx)
	elapsed := time.Since(startedAt)

	if err != nil {
		log.Error().Err(err).
			Str("event", "trial_sweep_job_failed").
			Int("suspended", suspended).
			Dur("duration", elapsed).
			Msg("trial sweep job completed with errors")

		return exitFailure
	}

	log.Info().
		Str("event", "trial_sweep_job_succeeded").
		Int("suspended", suspended).
		Dur("duration", elapsed).
		Msg("trial sweep job completed successfully")

	return exitSuccess
}

func shutdown(ctx context.Context, app *fx.App) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	return app.Stop(shutdownCtx)
}
