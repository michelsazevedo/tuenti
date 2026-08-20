package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

type deleteSpyItemRepository struct {
	deleteErr            error
	deleteCalls          int
	deleteOrganizationID pgtype.UUID
	deleteID             pgtype.UUID
}

func (r *deleteSpyItemRepository) Create(ctx context.Context, item *domain.Item) error {
	return errors.New("unexpected call to Create")
}

func (r *deleteSpyItemRepository) Update(ctx context.Context, item *domain.Item) error {
	return errors.New("unexpected call to Update")
}

func (r *deleteSpyItemRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	r.deleteCalls++
	r.deleteOrganizationID = organizationID
	r.deleteID = id

	return r.deleteErr
}

func (r *deleteSpyItemRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Item, error) {
	return nil, errors.New("unexpected call to FindByID")
}

func (r *deleteSpyItemRepository) List(ctx context.Context, filter domain.ListItemsFilter) ([]*domain.Item, error) {
	return nil, errors.New("unexpected call to List")
}

func TestDeleteItem(t *testing.T) {
	organizationID := pgtype.UUID{Bytes: [16]byte{0xaa, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	id := pgtype.UUID{Bytes: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	infraErr := errors.New("postgres: connection refused")

	tests := []struct {
		name      string
		deleteErr error
		wantErr   error
	}{
		{
			name: "deletes successfully",
		},
		{
			name:      "propagates not found unchanged",
			deleteErr: domain.ErrItemNotFound,
			wantErr:   domain.ErrItemNotFound,
		},
		{
			name:      "propagates cross-tenant deletes as not found",
			deleteErr: domain.ErrItemNotFound,
			wantErr:   domain.ErrItemNotFound,
		},
		{
			name:      "infrastructure failure propagates unchanged",
			deleteErr: infraErr,
			wantErr:   infraErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := &deleteSpyItemRepository{deleteErr: tt.deleteErr}

			err := NewDeleteItem(items).DeleteItem(context.Background(), organizationID, id)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, 1, items.deleteCalls)
			assert.Equal(t, organizationID, items.deleteOrganizationID)
			assert.Equal(t, id, items.deleteID)
		})
	}
}
