package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const itemColumns = `id, organization_id, name, COALESCE(description, '') AS description, type, rate,
	currency, income_account, track_inventory, quantity_in_stock, created_at, updated_at, deleted_at`

const itemTaxColumns = `id, item_id, name, COALESCE(tax_number, '') AS tax_number, rate, enabled, created_at, updated_at`

const createItem = `INSERT INTO items(id, organization_id, name, description, type, rate,
	currency, income_account, track_inventory, quantity_in_stock)
VALUES(COALESCE($1::uuid, uuid_generate_v4()), $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, created_at, updated_at`

const createItemTax = `INSERT INTO item_taxes(id, item_id, name, tax_number, rate, enabled)
VALUES(COALESCE($1::uuid, uuid_generate_v4()), $2, $3, $4, $5, $6)
RETURNING id, created_at, updated_at`

const updateItem = `UPDATE items
SET name = $3, description = $4, type = $5, rate = $6, currency = $7, income_account = $8,
	track_inventory = $9, quantity_in_stock = $10, updated_at = now()
WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL
RETURNING created_at, updated_at`

const deleteItemTaxes = `DELETE FROM item_taxes WHERE item_id = $1`

const findItemByID = `SELECT ` + itemColumns + `
FROM items WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL`

const findItemTaxes = `SELECT ` + itemTaxColumns + `
FROM item_taxes WHERE item_id = ANY($1) ORDER BY created_at, id`

const softDeleteItem = `UPDATE items SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL`

type ItemRepository struct {
	db database.DBTX
}

func NewItemRepository(db database.DBTX) *ItemRepository {
	return &ItemRepository{db: db}
}

func (r *ItemRepository) Create(ctx context.Context, item *domain.Item) error {
	args := append([]any{optionalUUID(item.ID)}, item.GetAttributes()...)

	err := r.db.QueryRow(ctx, createItem, args...).Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return err
	}

	return insertItemTaxes(ctx, r.db, item)
}

func (r *ItemRepository) Update(ctx context.Context, item *domain.Item) error {
	args := append([]any{item.ID}, item.GetAttributes()...)

	err := r.db.QueryRow(ctx, updateItem, args...).Scan(&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrItemNotFound
		}

		return err
	}

	if _, err := r.db.Exec(ctx, deleteItemTaxes, item.ID); err != nil {
		return err
	}

	return insertItemTaxes(ctx, r.db, item)
}

func (r *ItemRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	tag, err := r.db.Exec(ctx, softDeleteItem, id, organizationID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrItemNotFound
	}

	return nil
}

func (r *ItemRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Item, error) {
	item := &domain.Item{}

	if err := scanItem(r.db.QueryRow(ctx, findItemByID, id, organizationID), item); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrItemNotFound
		}

		return nil, err
	}

	if err := r.attachTaxes(ctx, []*domain.Item{item}); err != nil {
		return nil, err
	}

	return item, nil
}

func (r *ItemRepository) List(ctx context.Context, filter domain.ListItemsFilter) ([]*domain.Item, error) {
	query, args := buildListItemsQuery(filter)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Item, error) {
		item := &domain.Item{}

		if err := scanItem(row, item); err != nil {
			return nil, err
		}

		return item, nil
	})
	if err != nil {
		return nil, err
	}

	if err := r.attachTaxes(ctx, items); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *ItemRepository) attachTaxes(ctx context.Context, items []*domain.Item) error {
	for _, item := range items {
		item.Taxes = []domain.ItemTax{}
	}

	if len(items) == 0 {
		return nil
	}

	ids := make([]pgtype.UUID, 0, len(items))
	byID := make(map[pgtype.UUID]*domain.Item, len(items))

	for _, item := range items {
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}

	rows, err := r.db.Query(ctx, findItemTaxes, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var tax domain.ItemTax

		if err := scanItemTax(rows, &tax); err != nil {
			return err
		}

		if item, ok := byID[tax.ItemID]; ok {
			item.Taxes = append(item.Taxes, tax)
		}
	}

	return rows.Err()
}

func insertItemTaxes(ctx context.Context, db database.DBTX, item *domain.Item) error {
	for i := range item.Taxes {
		tax := &item.Taxes[i]
		tax.ItemID = item.ID

		args := append([]any{optionalUUID(tax.ID)}, tax.GetAttributes()...)

		err := db.QueryRow(ctx, createItemTax, args...).Scan(&tax.ID, &tax.CreatedAt, &tax.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return nil
}

func buildListItemsQuery(filter domain.ListItemsFilter) (string, []any) {
	var query strings.Builder

	query.WriteString(`SELECT ` + itemColumns + `
FROM items WHERE organization_id = $1 AND deleted_at IS NULL`)

	args := []any{filter.OrganizationID}

	if filter.Type != nil {
		args = append(args, *filter.Type)
		fmt.Fprintf(&query, " AND type = $%d", len(args))
	}

	if search := strings.TrimSpace(filter.Search); search != "" {
		args = append(args, search)
		fmt.Fprintf(&query, " AND name ILIKE '%%' || $%d || '%%'", len(args))
	}

	query.WriteString(" ORDER BY created_at DESC, id")

	return query.String(), args
}

func scanItem(row pgx.Row, item *domain.Item) error {
	var rate pgtype.Numeric

	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.Description, &item.Type, &rate,
		&item.Currency, &item.IncomeAccount, &item.TrackInventory, &item.QuantityInStock,
		&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
	)
	if err != nil {
		return err
	}

	item.Rate = numericToDecimal(rate)

	return nil
}

func scanItemTax(row pgx.Row, tax *domain.ItemTax) error {
	var rate pgtype.Numeric

	err := row.Scan(
		&tax.ID, &tax.ItemID, &tax.Name, &tax.TaxNumber, &rate,
		&tax.Enabled, &tax.CreatedAt, &tax.UpdatedAt,
	)
	if err != nil {
		return err
	}

	tax.Rate = numericToDecimal(rate)

	return nil
}

func optionalUUID(id pgtype.UUID) *pgtype.UUID {
	if !id.Valid {
		return nil
	}

	return &id
}

func numericToDecimal(n pgtype.Numeric) decimal.Decimal {
	if !n.Valid || n.NaN || n.Int == nil {
		return decimal.Zero
	}

	return decimal.NewFromBigInt(n.Int, n.Exp)
}
