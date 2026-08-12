package repository

import (
	"context"
	"testing"

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
	org := &domain.Organization{Name: "Acme " + randomSuffix(t)}

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
		assert.False(t, found.CreatedAt.IsZero(), "created_at must be hydrated")
		assert.False(t, found.UpdatedAt.IsZero(), "updated_at must be hydrated")
	})

	t.Run("maps a missing row to ErrOrganizationNotFound", func(t *testing.T) {
		found, err := repo.FindByID(ctx, randomUUID(t))

		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrOrganizationNotFound,
			"an absent row is a domain error, not a driver error")
	})
}
