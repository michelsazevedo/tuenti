package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createUser = `INSERT INTO users(name, email, password_digest) VALUES($1, $2, $3) RETURNING id`

const findUserByEmail = `SELECT id, name, email, password_digest, created_at, updated_at, confirmed_at FROM users WHERE email = $1`

const findUserByID = `SELECT id, name, email, password_digest, created_at, updated_at, confirmed_at FROM users WHERE id = $1`

const updateUserPasswordDigest = `UPDATE users SET password_digest = $2, updated_at = now() WHERE id = $1`

const markUserConfirmed = `UPDATE users SET confirmed_at = $2, updated_at = now() WHERE id = $1 AND confirmed_at IS NULL`

const uniqueViolationCode = "23505"

type UserRepository struct {
	db database.DBTX
}

func NewUserRepository(db database.DBTX) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	err := r.db.QueryRow(ctx, createUser, user.GetAttributes()...).Scan(&user.Id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return domain.ErrUserAlreadyExists
		}

		return err
	}

	return nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	user := &domain.User{}

	err := r.db.QueryRow(ctx, findUserByEmail, email).Scan(
		&user.Id, &user.Name, &user.Email, &user.PasswordDigest, &user.CreatedAt, &user.UpdatedAt, &user.ConfirmedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}

		return nil, err
	}

	return user, nil
}

func (r *UserRepository) FindByID(ctx context.Context, id pgtype.UUID) (*domain.User, error) {
	user := &domain.User{}

	err := r.db.QueryRow(ctx, findUserByID, id).Scan(
		&user.Id, &user.Name, &user.Email, &user.PasswordDigest, &user.CreatedAt, &user.UpdatedAt, &user.ConfirmedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}

		return nil, err
	}

	return user, nil
}

func (r *UserRepository) UpdatePasswordDigest(ctx context.Context, userID pgtype.UUID, passwordDigest string) error {
	_, err := r.db.Exec(ctx, updateUserPasswordDigest, userID, passwordDigest)

	return err
}

func (r *UserRepository) MarkConfirmed(ctx context.Context, id pgtype.UUID, at time.Time) error {
	_, err := r.db.Exec(ctx, markUserConfirmed, id, at)

	return err
}

var _ domain.UserRepository = (*UserRepository)(nil)
