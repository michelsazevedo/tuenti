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

type findSpyOrganizationRepository struct {
	organization *domain.Organization
	findErr      error
	findCalls    int
	findID       pgtype.UUID
}

func (r *findSpyOrganizationRepository) Create(ctx context.Context, org *domain.Organization) error {
	return errors.New("unexpected call to Create")
}

func (r *findSpyOrganizationRepository) FindByID(ctx context.Context, id pgtype.UUID) (*domain.Organization, error) {
	r.findCalls++
	r.findID = id

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.organization, nil
}

func TestGetOrganizationByID(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	organization := &domain.Organization{
		Id:        id,
		Name:      "Tuenti",
		CreatedAt: time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
	}
	infraErr := errors.New("postgres: connection refused")

	tests := []struct {
		name         string
		organization *domain.Organization
		findErr      error
		want         *domain.Organization
		wantErr      error
	}{
		{
			name:         "returns the organization unchanged",
			organization: organization,
			want:         organization,
		},
		{
			name:    "propagates not found unchanged",
			findErr: domain.ErrOrganizationNotFound,
			wantErr: domain.ErrOrganizationNotFound,
		},
		{
			name:    "infrastructure failure propagates unchanged",
			findErr: infraErr,
			wantErr: infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			organizations := &findSpyOrganizationRepository{organization: tt.organization, findErr: tt.findErr}

			got, err := NewGetOrganizationByID(organizations).GetByID(context.Background(), id)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.Same(t, tt.want, got)
			}

			assert.Equal(t, 1, organizations.findCalls)
			assert.Equal(t, id, organizations.findID)
		})
	}
}
