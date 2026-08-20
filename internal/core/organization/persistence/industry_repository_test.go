package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

var _ domain.IndustryRepository = (*IndustryRepository)(nil)

func TestIndustryRepositoryExists(t *testing.T) {
	pool := newTestPool(t)
	repo := NewIndustryRepository(pool)

	ctx := context.Background()

	t.Run("returns true for a stored industry", func(t *testing.T) {
		id := createTestIndustry(t, pool)

		exists, err := repo.Exists(ctx, id)

		require.NoError(t, err)
		assert.True(t, exists, "an industry that was just inserted must be visible to the catalog check")
	})

	t.Run("returns false for an unknown id", func(t *testing.T) {
		exists, err := repo.Exists(ctx, randomUUID(t))

		require.NoError(t, err, "an absent row is not an error: EXISTS always yields exactly one row")
		assert.False(t, exists)
	})
}
