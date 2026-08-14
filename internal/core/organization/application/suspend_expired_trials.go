package application

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const trialSweepBatchLimit = 0

type SuspendExpiredTrialsUseCase interface {
	Execute(ctx context.Context) (suspendedCount int, err error)
}

type suspendExpiredTrials struct {
	organizations domain.OrganizationRepository
}

func NewSuspendExpiredTrials(organizations domain.OrganizationRepository) SuspendExpiredTrialsUseCase {
	return &suspendExpiredTrials{organizations: organizations}
}

func (u *suspendExpiredTrials) Execute(ctx context.Context) (int, error) {
	expired, err := u.organizations.FindExpiredTrials(ctx, time.Now().UTC(), trialSweepBatchLimit)
	if err != nil {
		log.Error().Err(err).
			Str("event", "trial_sweep_scan_failed").
			Msg("could not read expired trials, nothing suspended")

		return 0, err
	}

	found, suspended, failed := len(expired), 0, 0

	for _, organization := range expired {
		if err := u.suspend(ctx, organization); err != nil {
			failed++

			log.Error().Err(err).
				Str("event", "trial_suspension_failed").
				Str("organization_id", organization.Id.String()).
				Msg("organization kept its previous status, continuing sweep")

			continue
		}

		suspended++
	}

	log.Info().
		Str("event", "trial_sweep_completed").
		Int("found", found).
		Int("suspended", suspended).
		Int("failed", failed).
		Msg("expired trial sweep finished")

	if failed > 0 {
		return suspended, fmt.Errorf("suspend expired trials: %d of %d organizations failed", failed, found)
	}

	return suspended, nil
}

func (u *suspendExpiredTrials) suspend(ctx context.Context, organization *domain.Organization) error {
	if err := organization.Suspend(); err != nil {
		return err
	}

	return u.organizations.UpdateSubscriptionStatus(ctx, organization.Id, organization.SubscriptionStatus)
}
