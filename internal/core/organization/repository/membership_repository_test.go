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
		Role:           domain.RoleManager,
	}

	require.NoError(t, repo.Create(ctx, membership))
	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, membership.Id)

	assert.True(t, membership.Id.Valid, "the database must hand back the generated id")
	assert.NotEqual(t, pgtype.UUID{}.Bytes, membership.Id.Bytes, "the generated id must not be the zero UUID")

	var role string
	require.NoError(t,
		pool.QueryRow(ctx, `SELECT role FROM memberships WHERE id = $1`, membership.Id).Scan(&role),
	)
	assert.Equal(t, string(domain.RoleManager), role, "the Role value type must round-trip as text")
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
		Role:           domain.RoleManager,
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

func TestMembershipRepositoryFindByUserID(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	user := createTestUser(t, pool)

	created := &domain.Membership{
		OrganizationId: org.Id,
		UserId:         user.Id,
		Role:           domain.RoleManager,
	}

	require.NoError(t, repo.Create(ctx, created))
	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, created.Id)

	found, err := repo.FindByUserID(ctx, user.Id)

	require.NoError(t, err)
	require.NotNil(t, found)

	assert.Equal(t, created.Id, found.Id)
	assert.Equal(t, org.Id, found.OrganizationId, "the organization id is what token issuance depends on")
	assert.Equal(t, user.Id, found.UserId)
	assert.Equal(t, domain.RoleManager, found.Role, "the Role value type must round-trip out of text")
	assert.False(t, found.CreatedAt.IsZero())
	assert.False(t, found.UpdatedAt.IsZero())
}

func TestMembershipRepositoryFindByUserIDWithoutMembership(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	user := createTestUser(t, pool)

	found, err := repo.FindByUserID(context.Background(), user.Id)

	assert.Nil(t, found)
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
}

func TestMembershipRepositoryFindByUserIDUnknownUser(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	found, err := repo.FindByUserID(context.Background(), randomUUID(t))

	assert.Nil(t, found)
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
}

func TestMembershipRepositoryFindByUserAndOrganization(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	ctx := context.Background()

	user := createTestUser(t, pool)
	managed := createTestOrganization(t, pool)
	joined := createTestOrganization(t, pool)

	managerMembership := &domain.Membership{
		OrganizationId: managed.Id,
		UserId:         user.Id,
		Role:           domain.RoleManager,
	}
	require.NoError(t, repo.Create(ctx, managerMembership))
	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, managerMembership.Id)

	memberMembership := &domain.Membership{
		OrganizationId: joined.Id,
		UserId:         user.Id,
		Role:           domain.RoleMember,
	}
	require.NoError(t, repo.Create(ctx, memberMembership))
	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, memberMembership.Id)

	found, err := repo.FindByUserAndOrganization(ctx, user.Id, managed.Id)

	require.NoError(t, err)
	require.NotNil(t, found)

	assert.Equal(t, managerMembership.Id, found.Id)
	assert.Equal(t, managed.Id, found.OrganizationId, "the membership must be scoped to the organization queried")
	assert.Equal(t, user.Id, found.UserId)
	assert.Equal(t, domain.RoleManager, found.Role, "authorization reads this role, so the wrong org's role must not leak in")
	assert.False(t, found.CreatedAt.IsZero())
	assert.False(t, found.UpdatedAt.IsZero())

	found, err = repo.FindByUserAndOrganization(ctx, user.Id, joined.Id)

	require.NoError(t, err)
	require.NotNil(t, found)

	assert.Equal(t, memberMembership.Id, found.Id)
	assert.Equal(t, joined.Id, found.OrganizationId)
	assert.Equal(t, domain.RoleMember, found.Role, "the same user resolves to a different role per organization")
}

func TestMembershipRepositoryFindByUserAndOrganizationForeignOrganization(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	ctx := context.Background()

	user := createTestUser(t, pool)
	joined := createTestOrganization(t, pool)
	foreign := createTestOrganization(t, pool)

	membership := &domain.Membership{
		OrganizationId: joined.Id,
		UserId:         user.Id,
		Role:           domain.RoleManager,
	}
	require.NoError(t, repo.Create(ctx, membership))
	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, membership.Id)

	found, err := repo.FindByUserAndOrganization(ctx, user.Id, foreign.Id)

	assert.Nil(t, found, "no membership of another organization may be returned as a fallback")
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
}

func TestMembershipRepositoryFindByUserAndOrganizationWithoutMembership(t *testing.T) {
	pool := newTestPool(t)
	repo := NewMembershipRepository(pool)

	user := createTestUser(t, pool)
	org := createTestOrganization(t, pool)

	found, err := repo.FindByUserAndOrganization(context.Background(), user.Id, org.Id)

	assert.Nil(t, found)
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
}
