package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

var _ domain.InvitationRepository = (*InvitationRepository)(nil)

const testInvitationTTL = 72 * time.Hour

func TestInvitationRepositoryCreateAndFindByID(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	expiresAt := time.Now().UTC().Add(testInvitationTTL).Truncate(time.Microsecond)
	invitation := &domain.Invitation{
		OrganizationId:        org.Id,
		Email:                 "wile." + randomSuffix(t) + "@example.com",
		Role:                  domain.RoleAdmin,
		TokenDigest:           randomSuffix(t),
		InvitedByMembershipId: inviter.Id,
		ExpiresAt:             expiresAt,
	}

	require.NoError(t, repo.Create(ctx, invitation))
	deleteRow(t, pool, `DELETE FROM invitations WHERE id = $1`, invitation.Id)

	assert.True(t, invitation.Id.Valid, "the database must hand back the generated id")
	assert.NotEqual(t, pgtype.UUID{}.Bytes, invitation.Id.Bytes, "the generated id must not be the zero UUID")
	assert.False(t, invitation.CreatedAt.IsZero(), "created_at must be returned by the insert")

	found, err := repo.FindByID(ctx, invitation.Id)

	require.NoError(t, err)
	require.NotNil(t, found)

	assert.Equal(t, invitation.Id, found.Id)
	assert.Equal(t, org.Id, found.OrganizationId)
	assert.Equal(t, invitation.Email, found.Email)
	assert.Equal(t, domain.RoleAdmin, found.Role, "the Role value type must round-trip out of text")
	assert.Equal(t, invitation.TokenDigest, found.TokenDigest)
	assert.Equal(t, inviter.Id, found.InvitedByMembershipId)
	assert.WithinDuration(t, expiresAt, found.ExpiresAt, time.Millisecond)
	assert.Nil(t, found.AcceptedAt, "a fresh invitation is neither accepted")
	assert.Nil(t, found.RevokedAt, "nor revoked")
}

func TestInvitationRepositoryFindByIDUnknown(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	found, err := repo.FindByID(context.Background(), randomUUID(t))

	assert.Nil(t, found)
	assert.ErrorIs(t, err, domain.ErrInvitationNotFound)
}

func TestInvitationRepositoryFindByTokenDigest(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	created := createTestInvitation(t, pool, org, inviter, nil)

	found, err := repo.FindByTokenDigest(ctx, created.TokenDigest)

	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.Id, found.Id, "the digest is the only handle the acceptance flow has")

	found, err = repo.FindByTokenDigest(ctx, "digest-that-was-never-issued-"+randomSuffix(t))

	assert.Nil(t, found, "an unknown digest must not fall back to any invitation")
	assert.ErrorIs(t, err, domain.ErrInvitationNotFound)
}

func TestInvitationRepositoryFindPendingByEmailAndOrganization(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	pending := createTestInvitation(t, pool, org, inviter, nil)

	found, err := repo.FindPendingByEmailAndOrganization(ctx, pending.Email, org.Id)

	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, pending.Id, found.Id)
	assert.Nil(t, found.AcceptedAt)
	assert.Nil(t, found.RevokedAt)
}

func TestInvitationRepositoryFindPendingByEmailAndOrganizationIsCaseInsensitive(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	pending := createTestInvitation(t, pool, org, inviter, func(i *domain.Invitation) {
		i.Email = "Wile." + randomSuffix(t) + "@Example.com"
	})

	found, err := repo.FindPendingByEmailAndOrganization(ctx, strings.ToLower(pending.Email), org.Id)

	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, pending.Id, found.Id,
		"the lookup must agree with the case-insensitive uniqueness rule, or the caller sees a phantom conflict")
}

func TestInvitationRepositoryFindPendingByEmailAndOrganizationExcludesUnusable(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	otherOrg := createTestOrganization(t, pool)
	otherInviter := createTestMembership(t, pool, otherOrg)

	expired := createTestInvitation(t, pool, org, inviter, func(i *domain.Invitation) {
		i.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	})

	accepted := createTestInvitation(t, pool, org, inviter, nil)
	require.NoError(t, repo.MarkAccepted(ctx, accepted.Id, time.Now().UTC()))

	revoked := createTestInvitation(t, pool, org, inviter, nil)
	require.NoError(t, repo.MarkRevoked(ctx, revoked.Id, time.Now().UTC()))

	foreign := createTestInvitation(t, pool, otherOrg, otherInviter, nil)

	tests := []struct {
		name           string
		email          string
		organizationID pgtype.UUID
	}{
		{name: "expired", email: expired.Email, organizationID: org.Id},
		{name: "accepted", email: accepted.Email, organizationID: org.Id},
		{name: "revoked", email: revoked.Email, organizationID: org.Id},
		{name: "other organization", email: foreign.Email, organizationID: org.Id},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			found, err := repo.FindPendingByEmailAndOrganization(ctx, test.email, test.organizationID)

			assert.Nil(t, found, "a %s invitation must never be treated as pending", test.name)
			assert.ErrorIs(t, err, domain.ErrInvitationNotFound)
		})
	}
}

func TestInvitationRepositoryFindByOrganization(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	otherOrg := createTestOrganization(t, pool)
	otherInviter := createTestMembership(t, pool, otherOrg)

	oldest := createTestInvitation(t, pool, org, inviter, nil)
	middle := createTestInvitation(t, pool, org, inviter, nil)
	newest := createTestInvitation(t, pool, org, inviter, nil)
	foreign := createTestInvitation(t, pool, otherOrg, otherInviter, nil)

	now := time.Now().UTC()
	setCreatedAt(t, pool, oldest.Id, now.Add(-3*time.Hour))
	setCreatedAt(t, pool, middle.Id, now.Add(-2*time.Hour))
	setCreatedAt(t, pool, newest.Id, now.Add(-time.Hour))

	invitations, err := repo.FindByOrganization(ctx, org.Id)

	require.NoError(t, err)
	require.Len(t, invitations, 3, "the listing must be scoped to the organization queried")

	assert.Equal(t, []pgtype.UUID{newest.Id, middle.Id, oldest.Id},
		[]pgtype.UUID{invitations[0].Id, invitations[1].Id, invitations[2].Id},
		"the listing must be newest first")

	for _, invitation := range invitations {
		assert.Equal(t, org.Id, invitation.OrganizationId)
		assert.NotEqual(t, foreign.Id, invitation.Id, "another organization's invitation must not leak in")
	}
}

func TestInvitationRepositoryFindByOrganizationWithoutInvitations(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	org := createTestOrganization(t, pool)

	invitations, err := repo.FindByOrganization(context.Background(), org.Id)

	require.NoError(t, err, "an organization with no invitations is not an error")
	assert.NotNil(t, invitations, "callers serialize this straight to JSON, so it must be [] and not null")
	assert.Empty(t, invitations)
}

func TestInvitationRepositoryMarkAccepted(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)
	invitation := createTestInvitation(t, pool, org, inviter, nil)

	acceptedAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.MarkAccepted(ctx, invitation.Id, acceptedAt))

	found, err := repo.FindByID(ctx, invitation.Id)

	require.NoError(t, err)
	require.NotNil(t, found.AcceptedAt, "acceptance must be persisted, it is what blocks a replay")
	assert.WithinDuration(t, acceptedAt, *found.AcceptedAt, time.Millisecond)
	assert.Nil(t, found.RevokedAt, "accepting must not touch revoked_at")
}

func TestInvitationRepositoryMarkRevoked(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)
	invitation := createTestInvitation(t, pool, org, inviter, nil)

	revokedAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.MarkRevoked(ctx, invitation.Id, revokedAt))

	found, err := repo.FindByID(ctx, invitation.Id)

	require.NoError(t, err)
	require.NotNil(t, found.RevokedAt, "revocation must be persisted, it is what kills the outstanding link")
	assert.WithinDuration(t, revokedAt, *found.RevokedAt, time.Millisecond)
	assert.Nil(t, found.AcceptedAt, "revoking must not touch accepted_at")
}

func TestInvitationRepositoryMarkUnknownInvitation(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()
	now := time.Now().UTC()

	assert.ErrorIs(t, repo.MarkAccepted(ctx, randomUUID(t), now), domain.ErrInvitationNotFound)
	assert.ErrorIs(t, repo.MarkRevoked(ctx, randomUUID(t), now), domain.ErrInvitationNotFound)
}

func TestInvitationRepositoryCreateDuplicatePending(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	first := createTestInvitation(t, pool, org, inviter, nil)

	duplicate := newTestInvitation(t, org, inviter)
	duplicate.Email = first.Email

	err := repo.Create(ctx, duplicate)

	assert.ErrorIs(t, err, domain.ErrDuplicateInvitation,
		"a second pending invite for the same (organization, email) must surface as a domain error")
	assert.False(t, duplicate.Id.Valid, "a rejected insert must not populate an id")
}

func TestInvitationRepositoryCreateDuplicatePendingIgnoresEmailCase(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	first := createTestInvitation(t, pool, org, inviter, func(i *domain.Invitation) {
		i.Email = "Foo." + randomSuffix(t) + "@Example.com"
	})

	duplicate := newTestInvitation(t, org, inviter)
	duplicate.Email = strings.ToLower(first.Email)

	err := repo.Create(ctx, duplicate)

	assert.ErrorIs(t, err, domain.ErrDuplicateInvitation,
		"email case must not be a way around the one-pending-invitation rule")
}

func TestInvitationRepositoryCreateAllowsReinviteAfterRevoke(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	revoked := createTestInvitation(t, pool, org, inviter, nil)
	require.NoError(t, repo.MarkRevoked(ctx, revoked.Id, time.Now().UTC()))

	reinvite := newTestInvitation(t, org, inviter)
	reinvite.Email = revoked.Email

	require.NoError(t, repo.Create(ctx, reinvite),
		"the partial index must leave revoked invitations out of the uniqueness rule")
	deleteRow(t, pool, `DELETE FROM invitations WHERE id = $1`, reinvite.Id)
}

func TestInvitationRepositoryCreateTokenDigestCollisionIsNotADomainError(t *testing.T) {
	pool := newTestPool(t)
	repo := NewInvitationRepository(pool)

	ctx := context.Background()

	org := createTestOrganization(t, pool)
	inviter := createTestMembership(t, pool, org)

	first := createTestInvitation(t, pool, org, inviter, nil)

	collision := newTestInvitation(t, org, inviter)
	collision.TokenDigest = first.TokenDigest

	err := repo.Create(ctx, collision)

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrDuplicateInvitation,
		"a digest collision is a token generation bug, not a duplicate invitation the caller can retry")

	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "the raw driver error must survive for diagnosis")
	assert.Equal(t, uniqueViolationCode, pgErr.Code)
	assert.Equal(t, "invitations_token_digest_idx", pgErr.ConstraintName)
}

func createTestMembership(t *testing.T, pool *pgxpool.Pool, org *domain.Organization) *domain.Membership {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	user := createTestUser(t, pool)
	membership := &domain.Membership{
		OrganizationId: org.Id,
		UserId:         user.Id,
		Role:           domain.RoleManager,
	}
	require.NoError(t, NewMembershipRepository(pool).Create(ctx, membership))

	deleteRow(t, pool, `DELETE FROM memberships WHERE id = $1`, membership.Id)

	return membership
}

func newTestInvitation(t *testing.T, org *domain.Organization, inviter *domain.Membership) *domain.Invitation {
	t.Helper()

	suffix := randomSuffix(t)

	return &domain.Invitation{
		OrganizationId:        org.Id,
		Email:                 "wile." + suffix + "@example.com",
		Role:                  domain.RoleMember,
		TokenDigest:           "digest-" + suffix,
		InvitedByMembershipId: inviter.Id,
		ExpiresAt:             time.Now().UTC().Add(testInvitationTTL),
	}
}

func createTestInvitation(
	t *testing.T,
	pool *pgxpool.Pool,
	org *domain.Organization,
	inviter *domain.Membership,
	customize func(*domain.Invitation),
) *domain.Invitation {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	invitation := newTestInvitation(t, org, inviter)
	if customize != nil {
		customize(invitation)
	}

	require.NoError(t, NewInvitationRepository(pool).Create(ctx, invitation))

	deleteRow(t, pool, `DELETE FROM invitations WHERE id = $1`, invitation.Id)

	return invitation
}

func setCreatedAt(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID, createdAt time.Time) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	_, err := pool.Exec(ctx, `UPDATE invitations SET created_at = $2 WHERE id = $1`, id, createdAt)
	require.NoError(t, err)
}
