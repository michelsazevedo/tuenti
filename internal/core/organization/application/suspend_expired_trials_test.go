package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type statusUpdate struct {
	id     pgtype.UUID
	status domain.SubscriptionStatus
}

type sweepSpyOrganizationRepository struct {
	expired   []*domain.Organization
	findErr   error
	findCalls int
	findNow   time.Time
	findLimit int

	updateErrs map[pgtype.UUID]error
	updates    []statusUpdate
}

func (r *sweepSpyOrganizationRepository) Create(ctx context.Context, org *domain.Organization) error {
	return errors.New("unexpected call to Create")
}

func (r *sweepSpyOrganizationRepository) FindByID(ctx context.Context, id pgtype.UUID) (*domain.Organization, error) {
	return nil, errors.New("unexpected call to FindByID")
}

func (r *sweepSpyOrganizationRepository) FindExpiredTrials(ctx context.Context, now time.Time, limit int) ([]*domain.Organization, error) {
	r.findCalls++
	r.findNow = now
	r.findLimit = limit

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.expired, nil
}

func (r *sweepSpyOrganizationRepository) UpdateSubscriptionStatus(ctx context.Context, id pgtype.UUID, status domain.SubscriptionStatus) error {
	r.updates = append(r.updates, statusUpdate{id: id, status: status})

	return r.updateErrs[id]
}

func organizationID(seed byte) pgtype.UUID {
	id := pgtype.UUID{Valid: true}
	for i := range id.Bytes {
		id.Bytes[i] = seed
	}

	return id
}

func expiredTrial(seed byte) *domain.Organization {
	return &domain.Organization{
		Id:                 organizationID(seed),
		Name:               "Tuenti",
		TrialStartsAt:      time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC),
		TrialEndsAt:        time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
		SubscriptionStatus: domain.Trialing,
	}
}

func TestSuspendExpiredTrials(t *testing.T) {
	first, second, third := expiredTrial(0x01), expiredTrial(0x02), expiredTrial(0x03)
	persistenceErr := errors.New("postgres: connection refused")

	tests := []struct {
		name          string
		expired       []*domain.Organization
		findErr       error
		updateErrs    map[pgtype.UUID]error
		wantSuspended int
		wantErr       bool
		wantUpdates   []statusUpdate
	}{
		{
			name:        "no expired trials leaves the repository untouched",
			expired:     nil,
			wantUpdates: nil,
		},
		{
			name:          "suspends every expired trial it finds",
			expired:       []*domain.Organization{first, second, third},
			wantSuspended: 3,
			wantUpdates: []statusUpdate{
				{id: first.Id, status: domain.Suspended},
				{id: second.Id, status: domain.Suspended},
				{id: third.Id, status: domain.Suspended},
			},
		},
		{
			name:          "one persistence failure does not abort the run",
			expired:       []*domain.Organization{first, second, third},
			updateErrs:    map[pgtype.UUID]error{second.Id: persistenceErr},
			wantSuspended: 2,
			wantErr:       true,
			wantUpdates: []statusUpdate{
				{id: first.Id, status: domain.Suspended},
				{id: second.Id, status: domain.Suspended},
				{id: third.Id, status: domain.Suspended},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, organization := range tt.expired {
				organization.SubscriptionStatus = domain.Trialing
			}

			organizations := &sweepSpyOrganizationRepository{expired: tt.expired, updateErrs: tt.updateErrs}

			suspended, err := NewSuspendExpiredTrials(organizations).Execute(context.Background())

			if tt.wantErr {
				assert.Error(t, err, "a run with per-organization failures must report an aggregate error")
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.wantSuspended, suspended)
			assert.Equal(t, tt.wantUpdates, organizations.updates)
			assert.Equal(t, 1, organizations.findCalls)
			assert.Equal(t, trialSweepBatchLimit, organizations.findLimit, "the repository must own the batch size")
			assert.Equal(t, time.UTC, organizations.findNow.Location(), "the sweep clock must be UTC")
		})
	}
}

func TestSuspendExpiredTrialsWhenScanFails(t *testing.T) {
	scanErr := errors.New("postgres: connection refused")
	organizations := &sweepSpyOrganizationRepository{findErr: scanErr}

	suspended, err := NewSuspendExpiredTrials(organizations).Execute(context.Background())

	assert.ErrorIs(t, err, scanErr)
	assert.Zero(t, suspended)
	assert.Empty(t, organizations.updates, "a failed scan must not attempt any update")
}

func TestSuspendExpiredTrialsWhenTransitionIsRejected(t *testing.T) {
	canceled := expiredTrial(0x04)
	canceled.SubscriptionStatus = domain.Canceled

	organizations := &sweepSpyOrganizationRepository{expired: []*domain.Organization{canceled}}

	suspended, err := NewSuspendExpiredTrials(organizations).Execute(context.Background())

	assert.Error(t, err)
	assert.Zero(t, suspended)
	assert.Empty(t, organizations.updates, "a rejected transition must never be persisted")
	assert.Equal(t, domain.Canceled, canceled.SubscriptionStatus, "the aggregate must be left untouched")
}
