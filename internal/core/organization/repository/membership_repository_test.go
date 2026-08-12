package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

var _ domain.MembershipRepository = (*MembershipRepository)(nil)

func TestMembershipRepositoryCreate(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	user := createTestUser(t, pool)

	membership := &domain.Membership{
		OrganizationId: org.Id,
		UserId:         user.Id,
		Role:           domain.RoleOwner,
	}

	require.NoError(t, repo.Create(ctx, membership))
	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, membership.Id)

	assert.True(t, membership.Id.Valid, "the database must hand back the generated id")
	assert.NotEqual(t, pgtype.UUID{}.Bytes, membership.Id.Bytes, "the generated id must not be the zero UUID")

	var role string
	require.NoError(t,
		pool.QueryRow(ctx, `SELECT role FROM memberships WHERE id = $1`, membership.Id).Scan(&role),
	)
	assert.Equal(t, string(domain.RoleOwner), role, "the Role value type must round-trip as text")
}

func TestMembershipRepositoryCreateDuplicate(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	user := createTestUser(t, pool)

	first := &domain.Membership{
		OrganizationId: org.Id,
		UserId:         user.Id,
		Role:           domain.RoleOwner,
	}

	require.NoError(t, repo.Create(ctx, first))
	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, first.Id)

	duplicate := &domain.Membership{
		OrganizationId: org.Id,
		UserId:         user.Id,
		Role:           domain.RoleAdmin,
	}

	err := repo.Create(ctx, duplicate)

	assert.ErrorIs(t, err, domain.ErrMembershipAlreadyExists,
		"the (organization_id, user_id) unique violation must surface as a domain error")
	assert.False(t, duplicate.Id.Valid, "a rejected insert must not populate an id")
}
