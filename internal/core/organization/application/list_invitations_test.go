package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

type listInvitationsSpyRepository struct {
	invitations []*domain.Invitation
	findErr     error
	findCalls   int
	findID      pgtype.UUID
}

func (r *listInvitationsSpyRepository) Create(ctx context.Context, invitation *domain.Invitation) error {
	return errors.New("unexpected call to Create")
}

func (r *listInvitationsSpyRepository) FindByID(ctx context.Context, id pgtype.UUID) (*domain.Invitation, error) {
	return nil, errors.New("unexpected call to FindByID")
}

func (r *listInvitationsSpyRepository) FindByTokenDigest(ctx context.Context, tokenDigest string) (*domain.Invitation, error) {
	return nil, errors.New("unexpected call to FindByTokenDigest")
}

func (r *listInvitationsSpyRepository) FindPendingByEmailAndOrganization(ctx context.Context, email string, organizationID pgtype.UUID) (*domain.Invitation, error) {
	return nil, errors.New("unexpected call to FindPendingByEmailAndOrganization")
}

func (r *listInvitationsSpyRepository) FindByOrganization(ctx context.Context, organizationID pgtype.UUID) ([]*domain.Invitation, error) {
	r.findCalls++
	r.findID = organizationID

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.invitations, nil
}

func (r *listInvitationsSpyRepository) MarkAccepted(ctx context.Context, id pgtype.UUID, acceptedAt time.Time) error {
	return errors.New("unexpected call to MarkAccepted")
}

func (r *listInvitationsSpyRepository) MarkRevoked(ctx context.Context, id pgtype.UUID, revokedAt time.Time) error {
	return errors.New("unexpected call to MarkRevoked")
}

func TestListInvitations(t *testing.T) {
	organizationID := pgtype.UUID{Bytes: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	createdAt := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	invitations := []*domain.Invitation{
		{
			Id:             pgtype.UUID{Bytes: [16]byte{0x11}, Valid: true},
			OrganizationId: organizationID,
			Email:          "newest@tuenti.com",
			Role:           domain.RoleAdmin,
			CreatedAt:      createdAt,
			ExpiresAt:      createdAt.Add(72 * time.Hour),
		},
		{
			Id:             pgtype.UUID{Bytes: [16]byte{0x12}, Valid: true},
			OrganizationId: organizationID,
			Email:          "oldest@tuenti.com",
			Role:           domain.RoleMember,
			CreatedAt:      createdAt.Add(-24 * time.Hour),
			ExpiresAt:      createdAt.Add(48 * time.Hour),
		},
	}
	infraErr := errors.New("postgres: connection refused")

	tests := []struct {
		name        string
		invitations []*domain.Invitation
		findErr     error
		want        []*domain.Invitation
		wantErr     error
	}{
		{
			name:        "returns the invitations unchanged",
			invitations: invitations,
			want:        invitations,
		},
		{
			name:        "returns an empty non-nil slice unchanged",
			invitations: []*domain.Invitation{},
			want:        []*domain.Invitation{},
		},
		{
			name:    "propagates not found unchanged",
			findErr: domain.ErrInvitationNotFound,
			wantErr: domain.ErrInvitationNotFound,
		},
		{
			name:    "infrastructure failure propagates unchanged",
			findErr: infraErr,
			wantErr: infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &listInvitationsSpyRepository{invitations: tt.invitations, findErr: tt.findErr}

			got, err := NewListInvitations(repository).ListInvitations(context.Background(), organizationID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				assert.Equal(t, tt.want, got)

				for i := range tt.want {
					assert.Same(t, tt.want[i], got[i])
				}
			}

			assert.Equal(t, 1, repository.findCalls)
			assert.Equal(t, organizationID, repository.findID)
		})
	}
}
