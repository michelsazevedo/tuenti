package persistence

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

var _ domain.OrganizationRepository = (*OrganizationRepository)(nil)

func TestOrganizationRepositoryCreate(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOrganizationRepository(pool)

	ctx := context.Background()

	now := time.Now().UTC()
	org := &domain.Organization{
		Name:               "Acme " + randomSuffix(t),
		IndustryID:         createTestIndustry(t, pool),
		NumberOfEmployees:  10,
		TrialStartsAt:      now,
		TrialEndsAt:        now.Add(testTrialDuration),
		SubscriptionStatus: domain.Trialing,
	}

	require.NoError(t, repo.Create(ctx, org))
	deleteRow(t, pool, `DELETE FROM organizations WHERE id = $1`, org.Id)

	assert.True(t, org.Id.Valid, "the database must hand back the generated id")
	assert.NotEqual(t, pgtype.UUID{}.Bytes, org.Id.Bytes, "the generated id must not be the zero UUID")
}

func TestOrganizationRepositoryFindByID(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOrganizationRepository(pool)

	ctx := context.Background()

	t.Run("returns the stored organization", func(t *testing.T) {
		created := createTestOrganization(t, pool)

		found, err := repo.FindByID(ctx, created.Id)

		require.NoError(t, err)
		assert.Equal(t, created.Id, found.Id)
		assert.Equal(t, created.Name, found.Name)
		assert.Equal(t, created.IndustryID, found.IndustryID)
		assert.Equal(t, created.NumberOfEmployees, found.NumberOfEmployees)
		assert.False(t, found.CreatedAt.IsZero(), "created_at must be hydrated")
		assert.False(t, found.UpdatedAt.IsZero(), "updated_at must be hydrated")
	})

	t.Run("round-trips the trial and subscription fields", func(t *testing.T) {
		created := createTestOrganization(t, pool)

		found, err := repo.FindByID(ctx, created.Id)
		require.NoError(t, err)

		assert.Equal(t, domain.Trialing, found.SubscriptionStatus,
			"a typed status must survive the write/read round trip")

		assert.WithinDuration(t, created.TrialStartsAt, found.TrialStartsAt, time.Second)
		assert.WithinDuration(t, created.TrialEndsAt, found.TrialEndsAt, time.Second)
		assert.WithinDuration(t, found.TrialStartsAt.Add(testTrialDuration), found.TrialEndsAt, time.Second,
			"the stored trial must span 14 days")

		assert.False(t, found.PlanID.Valid, "a trialing organization has no plan yet, so plan_id stays NULL")
	})

	t.Run("maps a missing row to ErrOrganizationNotFound", func(t *testing.T) {
		found, err := repo.FindByID(ctx, randomUUID(t))

		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrOrganizationNotFound,
			"an absent row is a domain error, not a driver error")
	})
}

func TestOrganizationRepositoryFindExpiredTrials(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOrganizationRepository(pool)

	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)

	const scanLimit = 1_000

	t.Run("returns only trialing organizations whose trial already ended", func(t *testing.T) {
		expired := createTestOrganizationWithSubscription(t, pool, domain.Trialing, now.Add(-72*time.Hour))
		stillTrialing := createTestOrganizationWithSubscription(t, pool, domain.Trialing, now.Add(72*time.Hour))

		activePast := createTestOrganizationWithSubscription(t, pool, domain.Active, now.Add(-72*time.Hour))
		pastDuePast := createTestOrganizationWithSubscription(t, pool, domain.PastDue, now.Add(-72*time.Hour))
		suspendedPast := createTestOrganizationWithSubscription(t, pool, domain.Suspended, now.Add(-72*time.Hour))
		canceledPast := createTestOrganizationWithSubscription(t, pool, domain.Canceled, now.Add(-72*time.Hour))

		found, err := repo.FindExpiredTrials(ctx, now, scanLimit)
		require.NoError(t, err)

		ids := organizationIDs(found)
		assert.Contains(t, ids, expired.Id, "a trialing organization past its trial end must be swept")
		assert.NotContains(t, ids, stillTrialing.Id, "a trial that has not ended yet is not expired")
		assert.NotContains(t, ids, activePast.Id, "a paying organization is never swept, whatever its trial dates say")
		assert.NotContains(t, ids, pastDuePast.Id)
		assert.NotContains(t, ids, suspendedPast.Id, "an already suspended organization must not be swept again")
		assert.NotContains(t, ids, canceledPast.Id)
	})

	t.Run("hydrates every column of the returned organizations", func(t *testing.T) {
		trialEndsAt := now.Add(-96 * time.Hour)
		expired := createTestOrganizationWithSubscription(t, pool, domain.Trialing, trialEndsAt)

		found, err := repo.FindExpiredTrials(ctx, now, scanLimit)
		require.NoError(t, err)

		org := requireOrganizationInResult(t, found, expired.Id)
		assert.Equal(t, expired.Name, org.Name)
		assert.Equal(t, domain.Trialing, org.SubscriptionStatus)
		assert.False(t, org.CreatedAt.IsZero(), "created_at must be hydrated")
		assert.False(t, org.UpdatedAt.IsZero(), "updated_at must be hydrated")
		assert.WithinDuration(t, trialEndsAt, org.TrialEndsAt, time.Second)
		assert.WithinDuration(t, trialEndsAt.Add(-testTrialDuration), org.TrialStartsAt, time.Second)
		assert.False(t, org.PlanID.Valid, "a trialing organization has no plan yet, so plan_id stays NULL")
	})

	t.Run("excludes a trial ending exactly at now", func(t *testing.T) {
		boundary := createTestOrganizationWithSubscription(t, pool, domain.Trialing, now)

		found, err := repo.FindExpiredTrials(ctx, now, scanLimit)
		require.NoError(t, err)

		assert.NotContains(t, organizationIDs(found), boundary.Id,
			"trial_ends_at < now is strict, so the exact boundary is not yet expired")
	})

	t.Run("orders the oldest expiry first", func(t *testing.T) {
		older := createTestOrganizationWithSubscription(t, pool, domain.Trialing, now.Add(-30*24*time.Hour))
		newer := createTestOrganizationWithSubscription(t, pool, domain.Trialing, now.Add(-1*time.Hour))

		found, err := repo.FindExpiredTrials(ctx, now, scanLimit)
		require.NoError(t, err)

		ids := organizationIDs(found)
		olderAt, newerAt := slices.Index(ids, older.Id), slices.Index(ids, newer.Id)
		require.NotEqual(t, -1, olderAt)
		require.NotEqual(t, -1, newerAt)

		assert.Less(t, olderAt, newerAt,
			"batches must drain the longest-expired organizations first")
	})

	t.Run("caps the result set at the requested limit", func(t *testing.T) {
		const limit = 2

		for i := range limit + 2 {
			createTestOrganizationWithSubscription(t, pool, domain.Trialing,
				now.Add(-time.Duration(i+1)*time.Hour))
		}

		found, err := repo.FindExpiredTrials(ctx, now, limit)
		require.NoError(t, err)

		assert.Len(t, found, limit, "the limit must bound what a single batch loads into memory")
	})

	t.Run("returns an empty slice when nothing has expired", func(t *testing.T) {
		found, err := repo.FindExpiredTrials(ctx, time.Unix(0, 0).UTC(), scanLimit)

		require.NoError(t, err)
		assert.NotNil(t, found, "callers must be able to range over the result without a nil check")
		assert.Empty(t, found)
	})
}

func TestOrganizationRepositoryUpdateSubscriptionStatus(t *testing.T) {
	pool := newTestPool(t)
	repo := NewOrganizationRepository(pool)

	ctx := context.Background()

	t.Run("persists the new status and bumps updated_at", func(t *testing.T) {
		created := createTestOrganization(t, pool)

		before, err := repo.FindByID(ctx, created.Id)
		require.NoError(t, err)
		require.Equal(t, domain.Trialing, before.SubscriptionStatus)

		require.NoError(t, repo.UpdateSubscriptionStatus(ctx, created.Id, domain.Suspended))

		after, err := repo.FindByID(ctx, created.Id)
		require.NoError(t, err)

		assert.Equal(t, domain.Suspended, after.SubscriptionStatus)
		assert.True(t, after.UpdatedAt.After(before.UpdatedAt),
			"the write must refresh updated_at, otherwise the row looks untouched")
		assert.WithinDuration(t, before.TrialEndsAt, after.TrialEndsAt, time.Second,
			"the status write must not disturb the trial window")
	})

	t.Run("maps a missing row to ErrOrganizationNotFound", func(t *testing.T) {
		err := repo.UpdateSubscriptionStatus(ctx, randomUUID(t), domain.Suspended)

		assert.ErrorIs(t, err, domain.ErrOrganizationNotFound,
			"an update that matches no row is a domain error, not a silent success")
	})
}

func organizationIDs(orgs []*domain.Organization) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(orgs))
	for _, org := range orgs {
		ids = append(ids, org.Id)
	}

	return ids
}

func requireOrganizationInResult(t *testing.T, orgs []*domain.Organization, id pgtype.UUID) *domain.Organization {
	t.Helper()

	for _, org := range orgs {
		if org.Id == id {
			return org
		}
	}

	t.Fatalf("organization %x is missing from the result set", id.Bytes)

	return nil
}
