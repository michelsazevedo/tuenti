package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identitydomain "github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	identitypersistence "github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	createInvitationTestTimeout = 10 * time.Second
	createInvitationBaseURL     = "https://app.example.com/invitations"
)

type createInvitationPublishedEvent struct {
	ctx   context.Context
	event domain.OrganizationInvitationCreated
}

type createInvitationRecordingPublisher struct {
	publishErr error
	published  []createInvitationPublishedEvent
}

func (p *createInvitationRecordingPublisher) PublishOrganizationInvitationCreated(
	ctx context.Context,
	event domain.OrganizationInvitationCreated,
) error {
	p.published = append(p.published, createInvitationPublishedEvent{ctx: ctx, event: event})

	return p.publishErr
}

func (p *createInvitationRecordingPublisher) only(t *testing.T) domain.OrganizationInvitationCreated {
	t.Helper()

	require.Len(t, p.published, 1, "exactly one invitation event must have been published")

	return p.published[0].event
}

func newCreateInvitationUseCase(
	pgConn *database.PgConn,
	publisher domain.InvitationEventPublisher,
) CreateInvitationUseCase {
	return NewCreateInvitation(
		persistence.NewOrganizationRepository(pgConn.Pool()),
		identitypersistence.NewUserRepository(pgConn.Pool()),
		persistence.NewMembershipRepository(pgConn.Pool()),
		persistence.NewInvitationRepository(pgConn.Pool()),
		NewMembershipAuthorizationService(persistence.NewMembershipRepository(pgConn.Pool())),
		publisher,
		&config.Config{Settings: config.Settings{InvitationBaseURL: createInvitationBaseURL}},
	)
}

type createInvitationMember struct {
	userID       pgtype.UUID
	membershipID pgtype.UUID
	name         string
	email        string
}

type createInvitationFixture struct {
	pool             *pgxpool.Pool
	suffix           string
	organizationID   pgtype.UUID
	organizationName string
	inviter          createInvitationMember
	userIDs          []pgtype.UUID
}

func newCreateInvitationFixture(t *testing.T, pgConn *database.PgConn, inviterRole domain.Role) *createInvitationFixture {
	t.Helper()

	ctx := createInvitationTestContext(t)
	suffix := createInvitationRandomSuffix(t)

	organization := &domain.Organization{
		Name:              "Acme " + suffix,
		IndustryID:        createInvitationTestIndustry(t, pgConn.Pool()),
		NumberOfEmployees: 10,
	}
	organization.StartTrial(time.Now().UTC())

	require.NoError(t, persistence.NewOrganizationRepository(pgConn.Pool()).Create(ctx, organization),
		"seeding the organization must succeed")

	fixture := &createInvitationFixture{
		pool:             pgConn.Pool(),
		suffix:           suffix,
		organizationID:   organization.Id,
		organizationName: organization.Name,
	}

	t.Cleanup(func() { fixture.cleanup(t) })

	fixture.inviter = fixture.addMember(t, "inviter."+suffix+"@example.com", inviterRole)

	return fixture
}

func (f *createInvitationFixture) addMember(t *testing.T, email string, role domain.Role) createInvitationMember {
	t.Helper()

	return f.addNamedMember(t, "Seeded "+f.suffix, email, role)
}

func (f *createInvitationFixture) addNamedMember(
	t *testing.T,
	name, email string,
	role domain.Role,
) createInvitationMember {
	t.Helper()

	ctx := createInvitationTestContext(t)

	user := &identitydomain.User{Name: name, Email: email, PasswordDigest: "seeded-digest"}
	require.NoError(t, identitypersistence.NewUserRepository(f.pool).Create(ctx, user), "seeding the user must succeed")

	f.userIDs = append(f.userIDs, user.Id)

	membership := &domain.Membership{OrganizationId: f.organizationID, UserId: user.Id, Role: role}
	require.NoError(t, persistence.NewMembershipRepository(f.pool).Create(ctx, membership),
		"seeding the membership must succeed")

	return createInvitationMember{userID: user.Id, membershipID: membership.Id, name: name, email: email}
}

func (f *createInvitationFixture) inviteeEmail(local string) string {
	return local + "." + f.suffix + "@example.com"
}

func (f *createInvitationFixture) countInvitations(t *testing.T, email string) int {
	t.Helper()

	var invitations int

	require.NoError(t, f.pool.QueryRow(createInvitationTestContext(t),
		`SELECT count(*) FROM invitations WHERE organization_id = $1 AND email = $2`,
		f.organizationID, email).Scan(&invitations))

	return invitations
}

type createInvitationRow struct {
	organizationID        pgtype.UUID
	email                 string
	role                  domain.Role
	tokenDigest           string
	invitedByMembershipID pgtype.UUID
	createdAt             time.Time
	expiresAt             time.Time
	acceptedAt            *time.Time
	revokedAt             *time.Time
}

func (f *createInvitationFixture) requireInvitationRow(t *testing.T, id pgtype.UUID) createInvitationRow {
	t.Helper()

	var row createInvitationRow

	require.NoError(t, f.pool.QueryRow(createInvitationTestContext(t), `
		SELECT organization_id, email, role, token_digest, invited_by_membership_id,
			created_at, expires_at, accepted_at, revoked_at
		FROM invitations WHERE id = $1
	`, id).Scan(
		&row.organizationID, &row.email, &row.role, &row.tokenDigest, &row.invitedByMembershipID,
		&row.createdAt, &row.expiresAt, &row.acceptedAt, &row.revokedAt,
	), "the invitation must have been persisted")

	return row
}

func (f *createInvitationFixture) cleanup(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), createInvitationTestTimeout)
	defer cancel()

	for _, statement := range []struct {
		sql string
		arg any
	}{
		{`DELETE FROM invitations WHERE organization_id = $1`, f.organizationID},
		{`DELETE FROM memberships WHERE organization_id = $1`, f.organizationID},
		{`DELETE FROM organizations WHERE id = $1`, f.organizationID},
		{`DELETE FROM users WHERE id = ANY($1)`, f.userIDs},
	} {
		if _, err := f.pool.Exec(ctx, statement.sql, statement.arg); err != nil {
			t.Errorf("cleanup failed for %q: %v", statement.sql, err)
		}
	}
}

func rawTokenFromCreateInvitationURL(t *testing.T, invitationURL string) string {
	t.Helper()

	require.True(t, strings.HasPrefix(invitationURL, createInvitationBaseURL+"?token="),
		"the link must be built from the configured base URL, got %q", invitationURL)

	parsed, err := url.Parse(invitationURL)
	require.NoError(t, err, "the invitation link must be a parseable URL")

	rawToken := parsed.Query().Get("token")
	require.NotEmpty(t, rawToken, "the link must carry a non-empty token")

	return rawToken
}

func newCreateInvitationTestPgConn(t *testing.T) *database.PgConn {
	t.Helper()

	setCreateInvitationDefaultEnv(t, "POSTGRES_HOST", "localhost:5432")
	setCreateInvitationDefaultEnv(t, "POSTGRES_USER", "tuenti")
	setCreateInvitationDefaultEnv(t, "POSTGRES_PASSWORD", "tuentipwd")
	setCreateInvitationDefaultEnv(t, "POSTGRES_DB", "tuenti")
	setCreateInvitationDefaultEnv(t, "PASSWORD_RESET_BASE_URL", "http://localhost:3000/reset-password")
	setCreateInvitationDefaultEnv(t, "EMAIL_CONFIRMATION_BASE_URL", "http://localhost:3000/confirm-email")
	setCreateInvitationDefaultEnv(t, "INVITATION_BASE_URL", "http://localhost:3000/invitations")

	conf, err := config.NewConfig()
	require.NoError(t, err, "invalid test configuration")

	pgConn, err := database.NewPgConn(conf)
	require.NoError(t, err, "failed to build the connection pool")

	ctx, cancel := context.WithTimeout(context.Background(), createInvitationTestTimeout)
	defer cancel()

	if err := pgConn.Ping(ctx); err != nil {
		pgConn.Close()
		t.Fatalf("postgres unavailable at %s: %v (start it with `docker compose up -d db`)",
			conf.Database.Host, err)
	}

	t.Cleanup(pgConn.Close)

	return pgConn
}

func setCreateInvitationDefaultEnv(t *testing.T, key, value string) {
	t.Helper()

	if _, ok := os.LookupEnv(key); !ok {
		t.Setenv(key, value)
	}
}

func createInvitationRandomSuffix(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	return hex.EncodeToString(buf)
}

func createInvitationTestIndustry(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()

	suffix := createInvitationRandomSuffix(t)

	var id pgtype.UUID

	ctx := context.Background()
	err := pool.QueryRow(ctx,
		`INSERT INTO industries(name, slug) VALUES($1, $2) RETURNING id`,
		"Aerospace "+suffix, "aerospace_"+suffix,
	).Scan(&id)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM industries WHERE id = $1`, id)
	})

	return id
}

func createInvitationTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), createInvitationTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func TestCreateInvitationByAManagerPersistsTheInvitationAndPublishesTheEvent(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleMember, domain.RoleAdmin, domain.RoleManager} {
		t.Run(string(role), func(t *testing.T) {
			pgConn := newCreateInvitationTestPgConn(t)
			ctx := createInvitationTestContext(t)

			fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
			publisher := &createInvitationRecordingPublisher{}
			email := fixture.inviteeEmail("invitee")

			invitation, err := newCreateInvitationUseCase(pgConn, publisher).
				CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, role)

			require.NoError(t, err, "a manager may invite any role")
			require.NotNil(t, invitation)
			require.True(t, invitation.Id.Valid, "the invitation id must be returned by the insert")

			row := fixture.requireInvitationRow(t, invitation.Id)

			assert.Equal(t, fixture.organizationID, row.organizationID)
			assert.Equal(t, email, row.email)
			assert.Equal(t, role, row.role, "the invitation must carry the requested role")
			assert.Equal(t, fixture.inviter.membershipID, row.invitedByMembershipID,
				"the invitation must be attributed to the inviter membership")
			assert.Nil(t, row.acceptedAt, "a fresh invitation must not be accepted")
			assert.Nil(t, row.revokedAt, "a fresh invitation must not be revoked")
			assert.WithinDuration(t, row.createdAt.Add(7*24*time.Hour), row.expiresAt, 5*time.Second,
				"the invitation must live for 7 days from its creation")

			event := publisher.only(t)
			assert.Equal(t, domain.EventOrganizationInvitationCreated, event.Event)
			assert.NotEmpty(t, event.EventID, "every event must carry an identifier for deduplication")
			assert.Equal(t, invitation.Id.String(), event.InvitationID)
			assert.Equal(t, fixture.organizationID.String(), event.OrganizationID)
			assert.Equal(t, fixture.organizationName, event.OrganizationName)
			assert.Equal(t, fixture.inviter.userID.String(), event.InviterUserID)
			assert.Equal(t, fixture.inviter.name, event.InviterName,
				"the event must carry the inviter name looked up from the authorizing membership")
			assert.Equal(t, email, event.InviteeEmail)
			assert.Equal(t, string(role), event.Role)

			rawToken := rawTokenFromCreateInvitationURL(t, event.InviteURL)
			assert.NotEqual(t, rawToken, row.tokenDigest, "the raw token must never be stored")
			assert.Equal(t, domain.HashInvitationToken(rawToken), row.tokenDigest,
				"the published token must hash to the stored digest")
		})
	}
}

func TestCreateInvitationByAnAdminIsLimitedToAdminsAndMembers(t *testing.T) {
	tests := []struct {
		name    string
		role    domain.Role
		wantErr error
	}{
		{name: "admin may invite a member", role: domain.RoleMember},
		{name: "admin may invite an admin", role: domain.RoleAdmin},
		{name: "admin may not invite a manager", role: domain.RoleManager, wantErr: domain.ErrInvitationForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgConn := newCreateInvitationTestPgConn(t)
			ctx := createInvitationTestContext(t)

			fixture := newCreateInvitationFixture(t, pgConn, domain.RoleAdmin)
			publisher := &createInvitationRecordingPublisher{}
			email := fixture.inviteeEmail("invitee")

			invitation, err := newCreateInvitationUseCase(pgConn, publisher).
				CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, tt.role)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, invitation)
				assert.Zero(t, fixture.countInvitations(t, email), "a forbidden invitation must not be persisted")
				assert.Empty(t, publisher.published, "a forbidden invitation must never be published")

				return
			}

			require.NoError(t, err)
			require.NotNil(t, invitation)
			assert.Equal(t, 1, fixture.countInvitations(t, email))
			assert.Equal(t, string(tt.role), publisher.only(t).Role)
		})
	}
}

func TestCreateInvitationByAMemberIsForbidden(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleMember, domain.RoleAdmin, domain.RoleManager} {
		t.Run(string(role), func(t *testing.T) {
			pgConn := newCreateInvitationTestPgConn(t)
			ctx := createInvitationTestContext(t)

			fixture := newCreateInvitationFixture(t, pgConn, domain.RoleMember)
			publisher := &createInvitationRecordingPublisher{}
			email := fixture.inviteeEmail("invitee")

			invitation, err := newCreateInvitationUseCase(pgConn, publisher).
				CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, role)

			require.ErrorIs(t, err, domain.ErrInvitationForbidden, "a member may not invite anyone")
			assert.Nil(t, invitation)
			assert.Zero(t, fixture.countInvitations(t, email), "a forbidden invitation must not be persisted")
			assert.Empty(t, publisher.published, "a forbidden invitation must never be published")
		})
	}
}

func TestCreateInvitationForAnExistingMemberIsRejected(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	existing := fixture.addMember(t, fixture.inviteeEmail("existing"), domain.RoleMember)

	publisher := &createInvitationRecordingPublisher{}

	invitation, err := newCreateInvitationUseCase(pgConn, publisher).
		CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, existing.email, domain.RoleAdmin)

	require.ErrorIs(t, err, domain.ErrAlreadyMember, "an existing member cannot be invited again")
	assert.Nil(t, invitation)
	assert.Zero(t, fixture.countInvitations(t, existing.email), "no invitation row may survive the rollback")
	assert.Empty(t, publisher.published, "a rejected invitation must never be published")
}

func TestCreateInvitationWithAPendingInvitationIsRejected(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	useCase := newCreateInvitationUseCase(pgConn, &createInvitationRecordingPublisher{})
	email := fixture.inviteeEmail("invitee")

	first, err := useCase.CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, domain.RoleMember)
	require.NoError(t, err)
	require.NotNil(t, first)

	publisher := &createInvitationRecordingPublisher{}

	second, err := newCreateInvitationUseCase(pgConn, publisher).
		CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, domain.RoleAdmin)

	require.ErrorIs(t, err, domain.ErrDuplicateInvitation, "one live invitation per address and organization")
	assert.Nil(t, second)
	assert.Equal(t, 1, fixture.countInvitations(t, email), "the duplicate must not add a second row")
	assert.Empty(t, publisher.published, "a duplicate invitation must never be published")
}

func TestCreateInvitationSucceedsEvenWhenTheEventFailsToPublish(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	publisher := &createInvitationRecordingPublisher{publishErr: errors.New("kafka unavailable")}
	email := fixture.inviteeEmail("invitee")

	invitation, err := newCreateInvitationUseCase(pgConn, publisher).
		CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, domain.RoleMember)

	require.NoError(t, err, "a failing publisher must not fail the invitation")
	require.NotNil(t, invitation)

	row := fixture.requireInvitationRow(t, invitation.Id)
	assert.Equal(t, email, row.email, "the invitation must survive a failed publish")

	assert.Equal(t, email, publisher.only(t).InviteeEmail, "the publish must have been attempted before it failed")
}

func TestCreateInvitationPublishesTheInviterNameLookedUpFromTheMembership(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	inviter := fixture.addNamedMember(t, "Grace Hopper", fixture.inviteeEmail("grace"), domain.RoleManager)

	publisher := &createInvitationRecordingPublisher{}
	email := fixture.inviteeEmail("invitee")

	invitation, err := newCreateInvitationUseCase(pgConn, publisher).
		CreateInvitation(ctx, inviter.userID, fixture.organizationID, email, domain.RoleMember)

	require.NoError(t, err)
	require.NotNil(t, invitation)

	event := publisher.only(t)
	assert.Equal(t, "Grace Hopper", event.InviterName,
		"the event must name the inviting user, not the seeded fixture default")
	assert.Equal(t, inviter.userID.String(), event.InviterUserID,
		"the event must identify the inviting user behind the authorizing membership")
	assert.NotEqual(t, fixture.inviter.name, event.InviterName,
		"the lookup must resolve the acting membership, not any member of the organization")
}

func TestCreateInvitationByANonMemberPropagatesTheMembershipError(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	outsider := newCreateInvitationFixture(t, pgConn, domain.RoleManager)

	publisher := &createInvitationRecordingPublisher{}
	email := fixture.inviteeEmail("invitee")

	invitation, err := newCreateInvitationUseCase(pgConn, publisher).
		CreateInvitation(ctx, outsider.inviter.userID, fixture.organizationID, email, domain.RoleMember)

	require.ErrorIs(t, err, domain.ErrMembershipNotFound,
		"an inviter without a membership in the target organization must be rejected by authorization")
	assert.Nil(t, invitation)
	assert.Zero(t, fixture.countInvitations(t, email))
	assert.Empty(t, publisher.published)
}
