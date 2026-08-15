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
	"github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const (
	createInvitationTestTimeout = 10 * time.Second
	createInvitationBaseURL     = "https://app.example.com/invitations"
)

type createInvitationSentEmail struct {
	ctx              context.Context
	email            string
	organizationName string
	role             string
	invitationURL    string
}

type createInvitationRecordingMailer struct {
	sendErr error
	sent    []createInvitationSentEmail
}

func (m *createInvitationRecordingMailer) SendInvitation(
	ctx context.Context,
	email, organizationName, role, invitationURL string,
) error {
	m.sent = append(m.sent, createInvitationSentEmail{
		ctx:              ctx,
		email:            email,
		organizationName: organizationName,
		role:             role,
		invitationURL:    invitationURL,
	})

	return m.sendErr
}

func (m *createInvitationRecordingMailer) only(t *testing.T) createInvitationSentEmail {
	t.Helper()

	require.Len(t, m.sent, 1, "exactly one invitation email must have been sent")

	return m.sent[0]
}

func newCreateInvitationUseCase(
	pgConn *database.PgConn,
	mailer domain.InvitationMailer,
) CreateInvitationUseCase {
	return NewCreateInvitation(
		database.NewUnitOfWork(pgConn),
		NewMembershipAuthorizationService(repository.NewMembershipRepository(pgConn.Pool())),
		mailer,
		&config.Config{Settings: config.Settings{InvitationBaseURL: createInvitationBaseURL}},
	)
}

type createInvitationMember struct {
	userID       pgtype.UUID
	membershipID pgtype.UUID
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

	organization := &domain.Organization{Name: "Acme " + suffix}
	organization.StartTrial(time.Now().UTC())

	require.NoError(t, repository.NewOrganizationRepository(pgConn.Pool()).Create(ctx, organization),
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

	ctx := createInvitationTestContext(t)

	user := &identitydomain.User{Name: "Seeded " + f.suffix, Email: email, PasswordDigest: "seeded-digest"}
	require.NoError(t, identitypersistence.NewUserRepository(f.pool).Create(ctx, user), "seeding the user must succeed")

	f.userIDs = append(f.userIDs, user.Id)

	membership := &domain.Membership{OrganizationId: f.organizationID, UserId: user.Id, Role: role}
	require.NoError(t, repository.NewMembershipRepository(f.pool).Create(ctx, membership),
		"seeding the membership must succeed")

	return createInvitationMember{userID: user.Id, membershipID: membership.Id, email: email}
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
	setCreateInvitationDefaultEnv(t, "RESEND_API_KEY", "test-key")
	setCreateInvitationDefaultEnv(t, "RESEND_FROM_EMAIL", "test@example.com")
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

func createInvitationTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), createInvitationTestTimeout)
	t.Cleanup(cancel)

	return ctx
}

func TestCreateInvitationByAManagerPersistsTheInvitationAndSendsTheEmail(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleMember, domain.RoleAdmin, domain.RoleManager} {
		t.Run(string(role), func(t *testing.T) {
			pgConn := newCreateInvitationTestPgConn(t)
			ctx := createInvitationTestContext(t)

			fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
			mailer := &createInvitationRecordingMailer{}
			email := fixture.inviteeEmail("invitee")

			invitation, err := newCreateInvitationUseCase(pgConn, mailer).
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

			sent := mailer.only(t)
			assert.Equal(t, email, sent.email, "the invitation must go to the invited address")
			assert.Equal(t, fixture.organizationName, sent.organizationName,
				"the email must name the organization the invitee is joining")
			assert.Equal(t, string(role), sent.role, "the email must state the offered role")

			rawToken := rawTokenFromCreateInvitationURL(t, sent.invitationURL)
			assert.NotEqual(t, rawToken, row.tokenDigest, "the raw token must never be stored")
			assert.Equal(t, domain.HashInvitationToken(rawToken), row.tokenDigest,
				"the emailed token must hash to the stored digest")
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
			mailer := &createInvitationRecordingMailer{}
			email := fixture.inviteeEmail("invitee")

			invitation, err := newCreateInvitationUseCase(pgConn, mailer).
				CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, tt.role)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, invitation)
				assert.Zero(t, fixture.countInvitations(t, email), "a forbidden invitation must not be persisted")
				assert.Empty(t, mailer.sent, "a forbidden invitation must never be emailed")

				return
			}

			require.NoError(t, err)
			require.NotNil(t, invitation)
			assert.Equal(t, 1, fixture.countInvitations(t, email))
			assert.Equal(t, string(tt.role), mailer.only(t).role)
		})
	}
}

func TestCreateInvitationByAMemberIsForbidden(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleMember, domain.RoleAdmin, domain.RoleManager} {
		t.Run(string(role), func(t *testing.T) {
			pgConn := newCreateInvitationTestPgConn(t)
			ctx := createInvitationTestContext(t)

			fixture := newCreateInvitationFixture(t, pgConn, domain.RoleMember)
			mailer := &createInvitationRecordingMailer{}
			email := fixture.inviteeEmail("invitee")

			invitation, err := newCreateInvitationUseCase(pgConn, mailer).
				CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, role)

			require.ErrorIs(t, err, domain.ErrInvitationForbidden, "a member may not invite anyone")
			assert.Nil(t, invitation)
			assert.Zero(t, fixture.countInvitations(t, email), "a forbidden invitation must not be persisted")
			assert.Empty(t, mailer.sent, "a forbidden invitation must never be emailed")
		})
	}
}

func TestCreateInvitationForAnExistingMemberIsRejected(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	existing := fixture.addMember(t, fixture.inviteeEmail("existing"), domain.RoleMember)

	mailer := &createInvitationRecordingMailer{}

	invitation, err := newCreateInvitationUseCase(pgConn, mailer).
		CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, existing.email, domain.RoleAdmin)

	require.ErrorIs(t, err, domain.ErrAlreadyMember, "an existing member cannot be invited again")
	assert.Nil(t, invitation)
	assert.Zero(t, fixture.countInvitations(t, existing.email), "no invitation row may survive the rollback")
	assert.Empty(t, mailer.sent, "a rejected invitation must never be emailed")
}

func TestCreateInvitationWithAPendingInvitationIsRejected(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	useCase := newCreateInvitationUseCase(pgConn, &createInvitationRecordingMailer{})
	email := fixture.inviteeEmail("invitee")

	first, err := useCase.CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, domain.RoleMember)
	require.NoError(t, err)
	require.NotNil(t, first)

	mailer := &createInvitationRecordingMailer{}

	second, err := newCreateInvitationUseCase(pgConn, mailer).
		CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, domain.RoleAdmin)

	require.ErrorIs(t, err, domain.ErrDuplicateInvitation, "one live invitation per address and organization")
	assert.Nil(t, second)
	assert.Equal(t, 1, fixture.countInvitations(t, email), "the duplicate must not add a second row")
	assert.Empty(t, mailer.sent, "a duplicate invitation must never be emailed")
}

func TestCreateInvitationSucceedsEvenWhenTheEmailFailsToSend(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	mailer := &createInvitationRecordingMailer{sendErr: errors.New("resend unavailable")}
	email := fixture.inviteeEmail("invitee")

	invitation, err := newCreateInvitationUseCase(pgConn, mailer).
		CreateInvitation(ctx, fixture.inviter.userID, fixture.organizationID, email, domain.RoleMember)

	require.NoError(t, err, "a failing mailer must not fail the invitation")
	require.NotNil(t, invitation)

	row := fixture.requireInvitationRow(t, invitation.Id)
	assert.Equal(t, email, row.email, "the invitation must survive a failed delivery so the link can be resent")

	sent := mailer.only(t)
	assert.Equal(t, email, sent.email, "the delivery must have been attempted before it failed")
}

func TestCreateInvitationByANonMemberPropagatesTheMembershipError(t *testing.T) {
	pgConn := newCreateInvitationTestPgConn(t)
	ctx := createInvitationTestContext(t)

	fixture := newCreateInvitationFixture(t, pgConn, domain.RoleManager)
	outsider := newCreateInvitationFixture(t, pgConn, domain.RoleManager)

	mailer := &createInvitationRecordingMailer{}
	email := fixture.inviteeEmail("invitee")

	invitation, err := newCreateInvitationUseCase(pgConn, mailer).
		CreateInvitation(ctx, outsider.inviter.userID, fixture.organizationID, email, domain.RoleMember)

	require.ErrorIs(t, err, domain.ErrMembershipNotFound,
		"an inviter without a membership in the target organization must be rejected by authorization")
	assert.Nil(t, invitation)
	assert.Zero(t, fixture.countInvitations(t, email))
	assert.Empty(t, mailer.sent)
}
