package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
)

type listSpyItemRepository struct {
	catalog    []*domain.Item
	listErr    error
	listCalls  int
	listFilter domain.ListItemsFilter
}

func (r *listSpyItemRepository) Create(ctx context.Context, item *domain.Item) error {
	return errors.New("unexpected call to Create")
}

func (r *listSpyItemRepository) Update(ctx context.Context, item *domain.Item) error {
	return errors.New("unexpected call to Update")
}

func (r *listSpyItemRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	return errors.New("unexpected call to Delete")
}

func (r *listSpyItemRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Item, error) {
	return nil, errors.New("unexpected call to FindByID")
}

func (r *listSpyItemRepository) List(ctx context.Context, filter domain.ListItemsFilter) ([]*domain.Item, error) {
	r.listCalls++
	r.listFilter = filter

	if r.listErr != nil {
		return nil, r.listErr
	}

	matches := make([]*domain.Item, 0, len(r.catalog))

	for _, item := range r.catalog {
		if item.OrganizationID != filter.OrganizationID {
			continue
		}

		if filter.Type != nil && item.Type != *filter.Type {
			continue
		}

		if search := strings.TrimSpace(filter.Search); search != "" &&
			!strings.Contains(strings.ToLower(item.Name), strings.ToLower(search)) {
			continue
		}

		matches = append(matches, item)
	}

	return matches, nil
}

func listItemsType(t domain.ItemType) *domain.ItemType {
	return &t
}

func listItemsNames(items []*domain.Item) []string {
	names := make([]string, 0, len(items))

	for _, item := range items {
		names = append(names, item.Name)
	}

	return names
}

func TestListItems(t *testing.T) {
	organizationID := pgtype.UUID{Bytes: [16]byte{0xaa, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}
	otherOrganizationID := pgtype.UUID{Bytes: [16]byte{0xbb, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}

	newItem := func(name string, kind domain.ItemType, owner pgtype.UUID) *domain.Item {
		return &domain.Item{
			OrganizationID: owner,
			Name:           name,
			Type:           kind,
			Rate:           decimal.NewFromInt(10),
			Currency:       "EUR",
		}
	}

	catalog := []*domain.Item{
		newItem("Espresso Machine", domain.ItemTypeItem, organizationID),
		newItem("Espresso Cleaning", domain.ItemTypeService, organizationID),
		newItem("Ceramic Mug", domain.ItemTypeItem, organizationID),
		newItem("Espresso Grinder", domain.ItemTypeItem, otherOrganizationID),
	}

	tests := []struct {
		name      string
		catalog   []*domain.Item
		filter    domain.ListItemsFilter
		wantNames []string
	}{
		{
			name:      "no filter returns every item in the organization",
			catalog:   catalog,
			filter:    domain.ListItemsFilter{OrganizationID: organizationID},
			wantNames: []string{"Espresso Machine", "Espresso Cleaning", "Ceramic Mug"},
		},
		{
			name:      "type filter narrows to that type",
			catalog:   catalog,
			filter:    domain.ListItemsFilter{OrganizationID: organizationID, Type: listItemsType(domain.ItemTypeService)},
			wantNames: []string{"Espresso Cleaning"},
		},
		{
			name:      "search filter narrows by name",
			catalog:   catalog,
			filter:    domain.ListItemsFilter{OrganizationID: organizationID, Search: "espresso"},
			wantNames: []string{"Espresso Machine", "Espresso Cleaning"},
		},
		{
			name:    "type and search filters combine",
			catalog: catalog,
			filter: domain.ListItemsFilter{
				OrganizationID: organizationID,
				Type:           listItemsType(domain.ItemTypeItem),
				Search:         "espresso",
			},
			wantNames: []string{"Espresso Machine"},
		},
		{
			name:      "no match returns an empty slice, not an error",
			catalog:   catalog,
			filter:    domain.ListItemsFilter{OrganizationID: organizationID, Search: "kombucha"},
			wantNames: []string{},
		},
		{
			name:      "empty catalog returns an empty slice, not an error",
			catalog:   nil,
			filter:    domain.ListItemsFilter{OrganizationID: organizationID},
			wantNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := &listSpyItemRepository{catalog: tt.catalog}

			got, err := NewListItems(items).ListItems(context.Background(), tt.filter)

			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, tt.wantNames, listItemsNames(got))
			assert.Equal(t, 1, items.listCalls)
			assert.Equal(t, tt.filter, items.listFilter)
		})
	}
}

func TestListItemsRejectsInvalidType(t *testing.T) {
	organizationID := pgtype.UUID{Bytes: [16]byte{0xaa, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}, Valid: true}

	tests := []struct {
		name     string
		itemType domain.ItemType
	}{
		{name: "unknown type", itemType: domain.ItemType("PRODUCT")},
		{name: "empty type", itemType: domain.ItemType("")},
		{name: "wrong case", itemType: domain.ItemType("item")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := &listSpyItemRepository{}

			got, err := NewListItems(items).ListItems(context.Background(), domain.ListItemsFilter{
				OrganizationID: organizationID,
				Type:           listItemsType(tt.itemType),
			})

			assert.ErrorIs(t, err, domain.ErrInvalidItemType)
			assert.Nil(t, got)
			assert.Equal(t, 0, items.listCalls, "repository must not be reached with an invalid type filter")
		})
	}
}

func TestListItemsPropagatesRepositoryFailure(t *testing.T) {
	infraErr := errors.New("postgres: connection refused")
	items := &listSpyItemRepository{listErr: infraErr}

	got, err := NewListItems(items).ListItems(context.Background(), domain.ListItemsFilter{})

	assert.ErrorIs(t, err, infraErr)
	assert.Nil(t, got)
	assert.Equal(t, 1, items.listCalls)
}
