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
	"golang.org/x/crypto/bcrypt"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	identitypersistence "github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	acceptInvitationTestTimeout     = 30 * time.Second
	acceptInvitationTestTTL         = 24 * time.Hour
	acceptInvitationTestEmailDomain = "@accept-invitation.example.com"
)

func setAcceptInvitationDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func acceptInvitationRandomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func acceptInvitationTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), acceptInvitationTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func newAcceptInvitationTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setAcceptInvitationDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setAcceptInvitationDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setAcceptInvitationDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setAcceptInvitationDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setAcceptInvitationDefaultEnv(t, "RESEND_API_KEY", "test-key")
	setAcceptInvitationDefaultEnv(t, "RESEND_FROM_EMAIL", "test@example.com")
	setAcceptInvitationDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setAcceptInvitationDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setAcceptInvitationDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), acceptInvitationTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	return pgConn
}

type acceptInvitationFixture struct {
	pool                *pgxpool.Pool
	useCase             AcceptInvitationUseCase
	suffix              string
	organizationID      pgtype.UUID
	inviterMembershipID pgtype.UUID
}

func newAcceptInvitationFixture(t *testing.T) *acceptInvitationFixture {
	t.Helper()

	pgConn := newAcceptInvitationTestPgConn(t)
	pool := pgConn.Pool()
	ctx := acceptInvitationTestContext(t)
	suffix := acceptInvitationRandomSuffix(t)

	organization := &domain.Organization{Name: "Acme " + suffix}
	organization.StartTrial(time.Now().UTC())
	require.NoError(t, repository.NewOrganizationRepository(pool).Create(ctx, organization),
		"the fixture organization must be seeded")

	inviter := &identitydomain.User{
		Name:           "Bugs Bunny " + suffix,
		Email:          "inviter." + suffix + acceptInvitationTestEmailDomain,
		PasswordDigest: "seeded-digest",
	}
	require.NoError(t, identitypersistence.NewUserRepository(pool).Create(ctx, inviter),
		"the throwaway inviter must be seeded")

	inviterMembership := &domain.Membership{
		OrganizationId: organization.Id,
		UserId:         inviter.Id,
		Role:           domain.RoleManager,
	}
	require.NoError(t, repository.NewMembershipRepository(pool).Create(ctx, inviterMembership),
		"the inviter membership is the invited_by_membership_id foreign key target")

	fixture := &acceptInvitationFixture{
		pool:                pool,
		useCase:             NewAcceptInvitation(database.NewUnitOfWork(pgConn)),
		suffix:              suffix,
		organizationID:      organization.Id,
		inviterMembershipID: inviterMembership.Id,
	}

	t.Cleanup(func() { fixture.cleanup(t) })

	return fixture
}

func (f *acceptInvitationFixture) cleanup(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), acceptInvitationTestTimeout)
	defer cancel()

	statements := []struct {
		sql string
		arg any
	}{
		{`DELETE FROM invitations WHERE organization_id = $1`, f.organizationID},
		{`DELETE FROM memberships WHERE organization_id = $1`, f.organizationID},
		{`DELETE FROM organizations WHERE id = $1`, f.organizationID},
		{`DELETE FROM users WHERE email LIKE $1`, "%" + f.suffix + acceptInvitationTestEmailDomain},
	}

	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement.sql, statement.arg); err != nil {
			t.Errorf("cleanup failed for %q: %v", statement.sql, err)
		}
	}
}

func (f *acceptInvitationFixture) email(local string) string {
	return local + "." + f.suffix + acceptInvitationTestEmailDomain
}

type acceptInvitationSeed struct {
	invitation *domain.Invitation
	rawToken   string
}

func (f *acceptInvitationFixture) seedInvitation(
	t *testing.T,
	email string,
	role domain.Role,
	expiresAt time.Time,
) acceptInvitationSeed {
	t.Helper()

	ctx := acceptInvitationTestContext(t)

	rawToken, err := domain.GenerateInvitationToken()
	require.NoError(t, err, "the seeded invitation needs a real token")

	invitation := &domain.Invitation{
		OrganizationId:        f.organizationID,
		Email:                 email,
		Role:                  role,
		TokenDigest:           domain.HashInvitationToken(rawToken),
		InvitedByMembershipId: f.inviterMembershipID,
		ExpiresAt:             expiresAt,
	}
	require.NoError(t, repository.NewInvitationRepository(f.pool).Create(ctx, invitation),
		"the invitation must be seeded")

	return acceptInvitationSeed{invitation: invitation, rawToken: rawToken}
}

func (f *acceptInvitationFixture) seedUser(t *testing.T, name, email string) *identitydomain.User {
	t.Helper()

	ctx := acceptInvitationTestContext(t)

	user := &identitydomain.User{Name: name, Email: email, PasswordDigest: "seeded-digest"}
	require.NoError(t, identitypersistence.NewUserRepository(f.pool).Create(ctx, user),
		"the existing user must be seeded")

	return user
}

func (f *acceptInvitationFixture) markInvitationRevoked(t *testing.T, id pgtype.UUID, at time.Time) {
	t.Helper()

	require.NoError(t, repository.NewInvitationRepository(f.pool).
		MarkRevoked(acceptInvitationTestContext(t), id, at))
}

func (f *acceptInvitationFixture) markInvitationAccepted(t *testing.T, id pgtype.UUID, at time.Time) {
	t.Helper()

	require.NoError(t, repository.NewInvitationRepository(f.pool).
		MarkAccepted(acceptInvitationTestContext(t), id, at))
}

func (f *acceptInvitationFixture) reloadInvitation(t *testing.T, id pgtype.UUID) *domain.Invitation {
	t.Helper()

	invitation, err := repository.NewInvitationRepository(f.pool).
		FindByID(acceptInvitationTestContext(t), id)
	require.NoError(t, err, "the seeded invitation must still be readable")

	return invitation
}

func (f *acceptInvitationFixture) countMemberships(t *testing.T, userID pgtype.UUID) int {
	t.Helper()

	var count int

	require.NoError(t, f.pool.QueryRow(acceptInvitationTestContext(t),
		`SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2`,
		userID, f.organizationID).Scan(&count))

	return count
}

func acceptInvitationStringPtr(value string) *string {
	return &value
}

func TestAcceptInvitationAddsAnExistingUserToTheOrganization(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	user := fixture.seedUser(t, "Daffy Duck "+fixture.suffix, fixture.email("daffy"))
	seed := fixture.seedInvitation(t, user.Email, domain.RoleAdmin, time.Now().UTC().Add(acceptInvitationTestTTL))

	membership, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken, nil, nil)
	require.NoError(t, err, "a pending invitation for an existing user must be accepted")
	require.NotNil(t, membership)

	assert.True(t, membership.Id.Valid, "the membership id must come back from the insert")
	assert.Equal(t, fixture.organizationID, membership.OrganizationId)
	assert.Equal(t, user.Id, membership.UserId)
	assert.Equal(t, domain.RoleAdmin, membership.Role, "the membership must inherit the invitation's role")

	persisted, err := repository.NewMembershipRepository(fixture.pool).
		FindByUserAndOrganization(ctx, user.Id, fixture.organizationID)
	require.NoError(t, err, "the membership must have been committed")
	assert.Equal(t, membership.Id, persisted.Id)
	assert.Equal(t, domain.RoleAdmin, persisted.Role)

	assert.NotNil(t, fixture.reloadInvitation(t, seed.invitation.Id).AcceptedAt,
		"a consumed invitation must not stay pending")
}

func TestAcceptInvitationSignsUpAndConfirmsANewUser(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	email := fixture.email("porky")
	seed := fixture.seedInvitation(t, email, domain.RoleMember, time.Now().UTC().Add(acceptInvitationTestTTL))

	name := "Porky Pig " + fixture.suffix
	password := "supersecret"

	membership, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken,
		acceptInvitationStringPtr(name), acceptInvitationStringPtr(password))
	require.NoError(t, err, "an unknown email with signup fields must be provisioned")
	require.NotNil(t, membership)

	user, err := identitypersistence.NewUserRepository(fixture.pool).FindByEmail(ctx, email)
	require.NoError(t, err, "the invited user must have been created")
	assert.Equal(t, name, user.Name)
	assert.NotNil(t, user.ConfirmedAt, "accepting an invitation proves the mailbox, so the account starts confirmed")
	assert.NotEqual(t, password, user.PasswordDigest, "the plaintext password must never be stored")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(password)),
		"the stored digest must verify against the supplied password")

	assert.Equal(t, user.Id, membership.UserId)
	assert.Equal(t, fixture.organizationID, membership.OrganizationId)
	assert.Equal(t, domain.RoleMember, membership.Role, "the membership must inherit the invitation's role")

	var organizations int
	require.NoError(t, fixture.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM organizations o
		JOIN memberships m ON m.organization_id = o.id
		WHERE m.user_id = $1
	`, user.Id).Scan(&organizations))
	assert.Equal(t, 1, organizations,
		"joining by invitation must not spin up an organization of the invitee's own")

	assert.Equal(t, 1, fixture.countMemberships(t, user.Id))
	assert.NotNil(t, fixture.reloadInvitation(t, seed.invitation.Id).AcceptedAt,
		"a consumed invitation must not stay pending")
}

func TestAcceptInvitationRequiresSignupFieldsForAnUnknownEmail(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)

	name := "Elmer Fudd " + fixture.suffix
	password := "supersecret"

	tests := []struct {
		scenario string
		name     *string
		password *string
	}{
		{scenario: "both omitted"},
		{scenario: "password omitted", name: acceptInvitationStringPtr(name)},
		{scenario: "name omitted", password: acceptInvitationStringPtr(password)},
		{scenario: "name blank", name: acceptInvitationStringPtr("   "), password: acceptInvitationStringPtr(password)},
		{scenario: "password blank", name: acceptInvitationStringPtr(name), password: acceptInvitationStringPtr("\t ")},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			ctx := acceptInvitationTestContext(t)

			email := fixture.email("elmer-" + acceptInvitationRandomSuffix(t))
			seed := fixture.seedInvitation(t, email, domain.RoleMember, time.Now().UTC().Add(acceptInvitationTestTTL))

			membership, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken, tt.name, tt.password)

			assert.ErrorIs(t, err, domain.ErrInvitationSignupFieldsRequired)
			assert.Nil(t, membership)

			_, err = identitypersistence.NewUserRepository(fixture.pool).FindByEmail(ctx, email)
			assert.ErrorIs(t, err, identitydomain.ErrUserNotFound, "a rejected signup must leave no account behind")

			assert.Nil(t, fixture.reloadInvitation(t, seed.invitation.Id).AcceptedAt,
				"a rejected acceptance must leave the invitation pending")
		})
	}
}

func TestAcceptInvitationRejectsSignupFieldsForAnAlreadyRegisteredEmail(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	user := fixture.seedUser(t, "Sylvester "+fixture.suffix, fixture.email("sylvester"))
	seed := fixture.seedInvitation(t, user.Email, domain.RoleMember, time.Now().UTC().Add(acceptInvitationTestTTL))

	membership, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken,
		acceptInvitationStringPtr("Sylvester Junior"), acceptInvitationStringPtr("supersecret"))

	assert.ErrorIs(t, err, domain.ErrInvitationEmailAlreadyRegistered,
		"signing up for an address that already has an account is a caller error, not a silent join")
	assert.Nil(t, membership)
	assert.Zero(t, fixture.countMemberships(t, user.Id), "the rejected call must create no membership")
	assert.Nil(t, fixture.reloadInvitation(t, seed.invitation.Id).AcceptedAt)
}

func TestAcceptInvitationRejectsAnExpiredInvitation(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	user := fixture.seedUser(t, "Tweety "+fixture.suffix, fixture.email("tweety"))
	seed := fixture.seedInvitation(t, user.Email, domain.RoleMember, time.Now().UTC().Add(-time.Minute))

	membership, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken, nil, nil)

	assert.ErrorIs(t, err, domain.ErrInvitationExpired)
	assert.Nil(t, membership)
	assert.Zero(t, fixture.countMemberships(t, user.Id), "an expired invitation must create no membership")
	assert.Nil(t, fixture.reloadInvitation(t, seed.invitation.Id).AcceptedAt)
}

func TestAcceptInvitationRejectsARevokedInvitation(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	user := fixture.seedUser(t, "Speedy Gonzales "+fixture.suffix, fixture.email("speedy"))
	seed := fixture.seedInvitation(t, user.Email, domain.RoleMember, time.Now().UTC().Add(acceptInvitationTestTTL))
	fixture.markInvitationRevoked(t, seed.invitation.Id, time.Now().UTC())

	membership, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken, nil, nil)

	assert.ErrorIs(t, err, domain.ErrInvitationRevoked)
	assert.Nil(t, membership)
	assert.Zero(t, fixture.countMemberships(t, user.Id), "a revoked invitation must create no membership")
}

func TestAcceptInvitationPropagatesAlreadyAcceptedWhenTheMembershipIsMissing(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	user := fixture.seedUser(t, "Marvin Martian "+fixture.suffix, fixture.email("marvin"))
	seed := fixture.seedInvitation(t, user.Email, domain.RoleMember, time.Now().UTC().Add(acceptInvitationTestTTL))
	fixture.markInvitationAccepted(t, seed.invitation.Id, time.Now().UTC())

	membership, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken, nil, nil)

	assert.ErrorIs(t, err, domain.ErrInvitationAlreadyAccepted)
	assert.Nil(t, membership)
	assert.Zero(t, fixture.countMemberships(t, user.Id))
}

func TestAcceptInvitationIsIdempotentOnDoubleSubmit(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	user := fixture.seedUser(t, "Road Runner "+fixture.suffix, fixture.email("road"))
	seed := fixture.seedInvitation(t, user.Email, domain.RoleAdmin, time.Now().UTC().Add(acceptInvitationTestTTL))

	first, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := fixture.useCase.AcceptInvitation(ctx, seed.rawToken, nil, nil)
	require.NoError(t, err, "replaying an accepted invitation must succeed, the side effect already happened")
	require.NotNil(t, second)

	assert.Equal(t, first.Id, second.Id, "the replay must return the membership that already exists")
	assert.Equal(t, domain.RoleAdmin, second.Role)
	assert.Equal(t, 1, fixture.countMemberships(t, user.Id), "the replay must not duplicate the membership")
}

func TestAcceptInvitationRejectsAnUnknownToken(t *testing.T) {
	fixture := newAcceptInvitationFixture(t)
	ctx := acceptInvitationTestContext(t)

	membership, err := fixture.useCase.AcceptInvitation(ctx, "not-a-real-token-"+fixture.suffix, nil, nil)

	assert.ErrorIs(t, err, domain.ErrInvitationNotFound)
	assert.Nil(t, membership)
}
