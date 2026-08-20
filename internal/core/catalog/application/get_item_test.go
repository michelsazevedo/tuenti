package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

type findSpyItemRepository struct {
	item               *domain.Item
	findErr            error
	findCalls          int
	findOrganizationID pgtype.UUID
	findID             pgtype.UUID
}

func (r *findSpyItemRepository) Create(ctx context.Context, item *domain.Item) error {
	return errors.New("unexpected call to Create")
}

func (r *findSpyItemRepository) Update(ctx context.Context, item *domain.Item) error {
	return errors.New("unexpected call to Update")
}

func (r *findSpyItemRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	return errors.New("unexpected call to Delete")
}

func (r *findSpyItemRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Item, error) {
	r.findCalls++
	r.findOrganizationID = organizationID
	r.findID = id

	if r.findErr != nil {
		return nil, r.findErr
	}

	return r.item, nil
}

func (r *findSpyItemRepository) List(ctx context.Context, filter domain.ListItemsFilter) ([]*domain.Item, error) {
	return nil, errors.New("unexpected call to List")
}

func TestGetItem(t *testing.T) {
	organizationID := pgtype.UUID{Bytes: [16]byte{0xaa, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	id := pgtype.UUID{Bytes: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	item := &domain.Item{
		ID:             id,
		OrganizationID: organizationID,
		Name:           "Espresso",
		Type:           domain.ItemTypeItem,
		Rate:           decimal.NewFromInt(12),
		Currency:       "EUR",
		CreatedAt:      time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
	}
	infraErr := errors.New("postgres: connection refused")

	tests := []struct {
		name    string
		item    *domain.Item
		findErr error
		want    *domain.Item
		wantErr error
	}{
		{
			name: "returns the item unchanged",
			item: item,
			want: item,
		},
		{
			name:    "propagates not found unchanged",
			findErr: domain.ErrItemNotFound,
			wantErr: domain.ErrItemNotFound,
		},
		{
			name:    "propagates cross-tenant lookups as not found",
			findErr: domain.ErrItemNotFound,
			wantErr: domain.ErrItemNotFound,
		},
		{
			name:    "infrastructure failure propagates unchanged",
			findErr: infraErr,
			wantErr: infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := &findSpyItemRepository{item: tt.item, findErr: tt.findErr}

			got, err := NewGetItem(items).GetItem(context.Background(), organizationID, id)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
			} else {
				assert.NoError(t, err)
				assert.Same(t, tt.want, got)
			}

			assert.Equal(t, 1, items.findCalls)
			assert.Equal(t, organizationID, items.findOrganizationID)
			assert.Equal(t, id, items.findID)
		})
	}
}
