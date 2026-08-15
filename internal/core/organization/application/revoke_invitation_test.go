package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	revokeInvitationTestTimeout = 10 * time.Second
	revokeInvitationTTL         = 72 * time.Hour
)

func newRevokeInvitationTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setRevokeInvitationDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setRevokeInvitationDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setRevokeInvitationDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setRevokeInvitationDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setRevokeInvitationDefaultEnv(t, "RESEND_API_KEY", "test-key")
	setRevokeInvitationDefaultEnv(t, "RESEND_FROM_EMAIL", "test@example.com")
	setRevokeInvitationDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setRevokeInvitationDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setRevokeInvitationDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), revokeInvitationTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	return pgConn
}

func setRevokeInvitationDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func revokeInvitationRandomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func revokeInvitationTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), revokeInvitationTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func revokeInvitationUnknownUUID(t *testing.T) pgtype.UUID {
	t.Helper()

	var id pgtype.UUID

	_, err := rand.Read(id.Bytes[:])
	require.NoError(t, err)

	id.Valid = true

	return id
}

type revokeInvitationOrg struct {
	pool                *pgxpool.Pool
	id                  pgtype.UUID
	inviterMembershipID pgtype.UUID
	userIDs             []pgtype.UUID
}

func newRevokeInvitationOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *revokeInvitationOrg {
	t.Helper()

	suffix := revokeInvitationRandomSuffix(t)

	org := &domain.Organization{Name: "Acme " + suffix}
	org.StartTrial(time.Now().UTC())

	require.NoError(t, repository.NewOrganizationRepository(pool).Create(ctx, org),
		"seeding the organization must succeed")

	seeded := &revokeInvitationOrg{pool: pool, id: org.Id}

	t.Cleanup(func() {
		seeded.cleanup(t)
	})

	_, seeded.inviterMembershipID = seeded.addMember(t, ctx, domain.RoleManager)

	return seeded
}

func (o *revokeInvitationOrg) addMember(
	t *testing.T, ctx context.Context, role domain.Role,
) (userID, membershipID pgtype.UUID) {
	t.Helper()

	userID = o.addUser(t, ctx, "Bugs Bunny")

	membership := &domain.Membership{OrganizationId: o.id, UserId: userID, Role: role}
	require.NoError(t, repository.NewMembershipRepository(o.pool).Create(ctx, membership),
		"seeding the membership must succeed")

	return userID, membership.Id
}

func (o *revokeInvitationOrg) addUser(t *testing.T, ctx context.Context, name string) pgtype.UUID {
	t.Helper()

	var userID pgtype.UUID

	userSuffix := revokeInvitationRandomSuffix(t)

	err := o.pool.QueryRow(ctx, `
		INSERT INTO users(name, email, password_digest)
		VALUES($1, $2, $3) RETURNING id
	`, name+" "+userSuffix, "user."+userSuffix+"@example.com", "digest").Scan(&userID)
	require.NoError(t, err, "seeding the user must succeed")

	o.userIDs = append(o.userIDs, userID)

	return userID
}

func (o *revokeInvitationOrg) createInvitation(
	t *testing.T, ctx context.Context, targetRole domain.Role,
) *domain.Invitation {
	t.Helper()

	inviteSuffix := revokeInvitationRandomSuffix(t)

	invitation := &domain.Invitation{
		OrganizationId:        o.id,
		Email:                 "invitee." + inviteSuffix + "@example.com",
		Role:                  targetRole,
		TokenDigest:           "digest-" + inviteSuffix,
		InvitedByMembershipId: o.inviterMembershipID,
		ExpiresAt:             time.Now().UTC().Add(revokeInvitationTTL),
	}

	require.NoError(t, repository.NewInvitationRepository(o.pool).Create(ctx, invitation),
		"seeding the invitation must succeed")

	return invitation
}

func (o *revokeInvitationOrg) setInvitationTimestamp(
	t *testing.T, ctx context.Context, id pgtype.UUID, column string, at time.Time,
) {
	t.Helper()

	sql := map[string]string{
		"accepted_at": `UPDATE invitations SET accepted_at = $2 WHERE id = $1`,
		"revoked_at":  `UPDATE invitations SET revoked_at = $2 WHERE id = $1`,
		"expires_at":  `UPDATE invitations SET expires_at = $2 WHERE id = $1`,
	}[column]
	require.NotEmpty(t, sql, "unsupported invitation timestamp column %q", column)

	tag, err := o.pool.Exec(ctx, sql, id, at)
	require.NoError(t, err, "the fixture update must succeed")
	require.EqualValues(t, 1, tag.RowsAffected(), "the fixture update must touch the seeded invitation")
}

func (o *revokeInvitationOrg) revokedAt(t *testing.T, ctx context.Context, id pgtype.UUID) *time.Time {
	t.Helper()

	var revokedAt *time.Time

	require.NoError(t, o.pool.QueryRow(ctx,
		`SELECT revoked_at FROM invitations WHERE id = $1`, id).Scan(&revokedAt),
		"the invitation must still exist")

	return revokedAt
}

func (o *revokeInvitationOrg) cleanup(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), revokeInvitationTestTimeout)
	defer cancel()

	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM invitations WHERE organization_id = $1`, []any{o.id}},
		{`DELETE FROM memberships WHERE organization_id = $1`, []any{o.id}},
		{`DELETE FROM organizations WHERE id = $1`, []any{o.id}},
		{`DELETE FROM users WHERE id = ANY($1)`, []any{o.userIDs}},
	} {
		if _, err := o.pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Errorf("cleanup failed for %q: %v", statement.sql, err)
		}
	}
}

func newRevokeInvitationUseCase(pgConn *database.PgConn) RevokeInvitationUseCase {
	return NewRevokeInvitation(
		database.NewUnitOfWork(pgConn),
		NewMembershipAuthorizationService(repository.NewMembershipRepository(pgConn.Pool())),
	)
}

func TestRevokeInvitationByRoleMatrix(t *testing.T) {
	testCases := []struct {
		name        string
		revokerRole domain.Role
		targetRole  domain.Role
	}{
		{name: "manager revokes an admin invitation", revokerRole: domain.RoleManager, targetRole: domain.RoleAdmin},
		{name: "manager revokes a manager invitation", revokerRole: domain.RoleManager, targetRole: domain.RoleManager},
		{name: "manager revokes a member invitation", revokerRole: domain.RoleManager, targetRole: domain.RoleMember},
		{name: "admin revokes a member invitation", revokerRole: domain.RoleAdmin, targetRole: domain.RoleMember},
		{name: "admin revokes an admin invitation", revokerRole: domain.RoleAdmin, targetRole: domain.RoleAdmin},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pgConn := newRevokeInvitationTestPgConn(t)
			ctx := revokeInvitationTestContext(t)

			org := newRevokeInvitationOrg(t, ctx, pgConn.Pool())
			revokerID, _ := org.addMember(t, ctx, testCase.revokerRole)
			invitation := org.createInvitation(t, ctx, testCase.targetRole)

			before := time.Now().UTC()

			require.NoError(t,
				newRevokeInvitationUseCase(pgConn).RevokeInvitation(ctx, revokerID, org.id, invitation.Id),
				"a %s must be allowed to revoke a %s invitation", testCase.revokerRole, testCase.targetRole)

			revokedAt := org.revokedAt(t, ctx, invitation.Id)
			require.NotNil(t, revokedAt, "a successful revoke must stamp revoked_at")
			assert.WithinDuration(t, before, *revokedAt, revokeInvitationTestTimeout,
				"revoked_at must be stamped at revocation time")
		})
	}
}

func TestRevokeInvitationWhenTheRevokerOutranksNobody(t *testing.T) {
	testCases := []struct {
		name        string
		revokerRole domain.Role
		targetRole  domain.Role
	}{
		{name: "admin cannot revoke a manager invitation", revokerRole: domain.RoleAdmin, targetRole: domain.RoleManager},
		{name: "member cannot revoke a member invitation", revokerRole: domain.RoleMember, targetRole: domain.RoleMember},
		{name: "member cannot revoke an admin invitation", revokerRole: domain.RoleMember, targetRole: domain.RoleAdmin},
		{name: "member cannot revoke a manager invitation", revokerRole: domain.RoleMember, targetRole: domain.RoleManager},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pgConn := newRevokeInvitationTestPgConn(t)
			ctx := revokeInvitationTestContext(t)

			org := newRevokeInvitationOrg(t, ctx, pgConn.Pool())
			revokerID, _ := org.addMember(t, ctx, testCase.revokerRole)
			invitation := org.createInvitation(t, ctx, testCase.targetRole)

			err := newRevokeInvitationUseCase(pgConn).RevokeInvitation(ctx, revokerID, org.id, invitation.Id)

			require.ErrorIs(t, err, domain.ErrInvitationForbidden,
				"a %s must not revoke a %s invitation", testCase.revokerRole, testCase.targetRole)
			assert.Nil(t, org.revokedAt(t, ctx, invitation.Id),
				"a rejected revoke must leave the invitation pending")
		})
	}
}

func TestRevokeInvitationWhenTheRevokerHasNoMembership(t *testing.T) {
	pgConn := newRevokeInvitationTestPgConn(t)
	ctx := revokeInvitationTestContext(t)

	org := newRevokeInvitationOrg(t, ctx, pgConn.Pool())
	outsiderID := org.addUser(t, ctx, "Elmer Fudd")
	invitation := org.createInvitation(t, ctx, domain.RoleMember)

	err := newRevokeInvitationUseCase(pgConn).RevokeInvitation(ctx, outsiderID, org.id, invitation.Id)

	require.ErrorIs(t, err, domain.ErrMembershipNotFound,
		"the authorization failure must propagate unchanged")
	assert.Nil(t, org.revokedAt(t, ctx, invitation.Id),
		"an unauthorized caller must not touch the invitation")
}

func TestRevokeInvitationIsANoOpForInvitationsAlreadyOutOfPlay(t *testing.T) {
	alreadyRevokedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	testCases := []struct {
		name             string
		prepare          func(t *testing.T, ctx context.Context, org *revokeInvitationOrg, invitation *domain.Invitation)
		expectedRevoked  *time.Time
		expectationLabel string
	}{
		{
			name: "already accepted",
			prepare: func(t *testing.T, ctx context.Context, org *revokeInvitationOrg, invitation *domain.Invitation) {
				org.setInvitationTimestamp(t, ctx, invitation.Id, "accepted_at", time.Now().UTC().Add(-time.Hour))
			},
			expectationLabel: "an accepted invitation must not also gain a revoked_at stamp",
		},
		{
			name: "already revoked",
			prepare: func(t *testing.T, ctx context.Context, org *revokeInvitationOrg, invitation *domain.Invitation) {
				org.setInvitationTimestamp(t, ctx, invitation.Id, "revoked_at", alreadyRevokedAt)
			},
			expectedRevoked:  &alreadyRevokedAt,
			expectationLabel: "a second revoke must not move the original revoked_at",
		},
		{
			name: "already expired",
			prepare: func(t *testing.T, ctx context.Context, org *revokeInvitationOrg, invitation *domain.Invitation) {
				org.setInvitationTimestamp(t, ctx, invitation.Id, "expires_at", time.Now().UTC().Add(-time.Minute))
			},
			expectationLabel: "an expired invitation has nothing left to revoke",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pgConn := newRevokeInvitationTestPgConn(t)
			ctx := revokeInvitationTestContext(t)

			org := newRevokeInvitationOrg(t, ctx, pgConn.Pool())
			revokerID, _ := org.addMember(t, ctx, domain.RoleManager)
			invitation := org.createInvitation(t, ctx, domain.RoleMember)

			testCase.prepare(t, ctx, org, invitation)

			require.NoError(t,
				newRevokeInvitationUseCase(pgConn).RevokeInvitation(ctx, revokerID, org.id, invitation.Id),
				"revoking an invitation that is already out of play must succeed as a no-op")

			revokedAt := org.revokedAt(t, ctx, invitation.Id)

			if testCase.expectedRevoked == nil {
				assert.Nil(t, revokedAt, testCase.expectationLabel)

				return
			}

			require.NotNil(t, revokedAt, testCase.expectationLabel)
			assert.WithinDuration(t, *testCase.expectedRevoked, *revokedAt, time.Second, testCase.expectationLabel)
		})
	}
}

func TestRevokeInvitationWhenTheInvitationDoesNotExist(t *testing.T) {
	pgConn := newRevokeInvitationTestPgConn(t)
	ctx := revokeInvitationTestContext(t)

	org := newRevokeInvitationOrg(t, ctx, pgConn.Pool())
	revokerID, _ := org.addMember(t, ctx, domain.RoleManager)

	err := newRevokeInvitationUseCase(pgConn).RevokeInvitation(
		ctx, revokerID, org.id, revokeInvitationUnknownUUID(t))

	assert.ErrorIs(t, err, domain.ErrInvitationNotFound,
		"an unknown invitation id must surface as not found")
}

func TestRevokeInvitationWhenTheInvitationBelongsToAnotherOrganization(t *testing.T) {
	pgConn := newRevokeInvitationTestPgConn(t)
	ctx := revokeInvitationTestContext(t)

	callerOrg := newRevokeInvitationOrg(t, ctx, pgConn.Pool())
	otherOrg := newRevokeInvitationOrg(t, ctx, pgConn.Pool())

	revokerID, _ := callerOrg.addMember(t, ctx, domain.RoleManager)
	foreignInvitation := otherOrg.createInvitation(t, ctx, domain.RoleMember)

	err := newRevokeInvitationUseCase(pgConn).RevokeInvitation(
		ctx, revokerID, callerOrg.id, foreignInvitation.Id)

	require.ErrorIs(t, err, domain.ErrInvitationNotFound,
		"a cross-organization id must read as missing, never as forbidden, so ids stay unenumerable")
	assert.Nil(t, otherOrg.revokedAt(t, ctx, foreignInvitation.Id),
		"the foreign invitation must be untouched")
}
