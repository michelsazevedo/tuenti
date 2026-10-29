package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

// uniqueViolation is the SQLSTATE Postgres raises when a unique index rejects a write.
// The only unique index on customers is (organization_id, lower(email)) for active rows.
const uniqueViolation = "23505"

const customerColumns = `id, organization_id, name, company_name, email, phone_number,
	COALESCE(street, '') AS street, COALESCE(complement, '') AS complement, COALESCE(city, '') AS city,
	COALESCE(state, '') AS state, COALESCE(country, '') AS country, created_at, updated_at, deleted_at`

const createCustomer = `INSERT INTO customers(id, organization_id, name, company_name, email, phone_number,
	street, complement, city, state, country)
VALUES(COALESCE($1::uuid, uuid_generate_v4()), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, created_at, updated_at`

const updateCustomer = `UPDATE customers
SET name = $3, company_name = $4, email = $5, phone_number = $6, street = $7, complement = $8,
	city = $9, state = $10, country = $11, updated_at = now()
WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL
RETURNING created_at, updated_at`

const findCustomerByID = `SELECT ` + customerColumns + `
FROM customers WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL`

const softDeleteCustomer = `UPDATE customers SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL`

type CustomerRepository struct {
	db database.DBTX
}

func NewCustomerRepository(db database.DBTX) *CustomerRepository {
	return &CustomerRepository{db: db}
}

func (r *CustomerRepository) Create(ctx context.Context, customer *domain.Customer) error {
	args := append([]any{optionalUUID(customer.ID), customer.OrganizationID}, customerAttributes(customer)...)

	err := r.db.QueryRow(ctx, createCustomer, args...).Scan(&customer.ID, &customer.CreatedAt, &customer.UpdatedAt)
	if err != nil {
		return translateWriteError(err)
	}

	return nil
}

func (r *CustomerRepository) Update(ctx context.Context, customer *domain.Customer) error {
	args := append([]any{customer.ID, customer.OrganizationID}, customerAttributes(customer)...)

	err := r.db.QueryRow(ctx, updateCustomer, args...).Scan(&customer.CreatedAt, &customer.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrCustomerNotFound
		}

		return translateWriteError(err)
	}

	return nil
}

func (r *CustomerRepository) Delete(ctx context.Context, organizationID, id pgtype.UUID) error {
	tag, err := r.db.Exec(ctx, softDeleteCustomer, id, organizationID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrCustomerNotFound
	}

	return nil
}

func (r *CustomerRepository) FindByID(ctx context.Context, organizationID, id pgtype.UUID) (*domain.Customer, error) {
	customer := &domain.Customer{}

	if err := scanCustomer(r.db.QueryRow(ctx, findCustomerByID, id, organizationID), customer); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCustomerNotFound
		}

		return nil, err
	}

	return customer, nil
}

func (r *CustomerRepository) List(ctx context.Context, filter domain.ListCustomersFilter) ([]*domain.Customer, error) {
	query, args := buildListCustomersQuery(filter)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Customer, error) {
		customer := &domain.Customer{}

		if err := scanCustomer(row, customer); err != nil {
			return nil, err
		}

		return customer, nil
	})
}

func buildListCustomersQuery(filter domain.ListCustomersFilter) (string, []any) {
	var query strings.Builder

	query.WriteString(`SELECT ` + customerColumns + `
FROM customers WHERE organization_id = $1 AND deleted_at IS NULL`)

	args := []any{filter.OrganizationID}

	if search := strings.TrimSpace(filter.Search); search != "" {
		args = append(args, search)
		fmt.Fprintf(&query,
			" AND (name ILIKE '%%' || $%[1]d || '%%' OR company_name ILIKE '%%' || $%[1]d || '%%'"+
				" OR email ILIKE '%%' || $%[1]d || '%%')",
			len(args))
	}

	query.WriteString(" ORDER BY created_at DESC, id")

	return query.String(), args
}

// customerAttributes returns the mutable columns in the positional order shared by
// createCustomer and updateCustomer ($3..$11). A nil address, or an empty address
// field, is stored as NULL.
func customerAttributes(customer *domain.Customer) []any {
	address := domain.Address{}
	if customer.Address != nil {
		address = *customer.Address
	}

	return []any{
		customer.Name, customer.CompanyName, customer.Email, customer.PhoneNumber,
		nullableText(address.Street), nullableText(address.Complement), nullableText(address.City),
		nullableText(address.State), nullableText(address.Country),
	}
}

func scanCustomer(row pgx.Row, customer *domain.Customer) error {
	var address domain.Address

	err := row.Scan(
		&customer.ID, &customer.OrganizationID, &customer.Name, &customer.CompanyName,
		&customer.Email, &customer.PhoneNumber,
		&address.Street, &address.Complement, &address.City, &address.State, &address.Country,
		&customer.CreatedAt, &customer.UpdatedAt, &customer.DeletedAt,
	)
	if err != nil {
		return err
	}

	customer.Address = nil
	if address != (domain.Address{}) {
		customer.Address = &address
	}

	return nil
}

func translateWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrCustomerEmailTaken
	}

	return err
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func optionalUUID(id pgtype.UUID) *pgtype.UUID {
	if !id.Valid {
		return nil
	}

	return &id
}
