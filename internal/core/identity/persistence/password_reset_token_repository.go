package persistence

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createPasswordResetToken = `INSERT INTO password_reset_tokens(user_id, token_digest, expires_at) VALUES($1, $2, $3) RETURNING id, created_at`

const findPasswordResetTokenByDigest = `SELECT id, user_id, token_digest, expires_at, used_at, created_at FROM password_reset_tokens WHERE token_digest = $1`

const markPasswordResetTokenUsed = `UPDATE password_reset_tokens SET used_at = now() WHERE id = $1 AND used_at IS NULL`

const invalidateActivePasswordResetTokensForUser = `UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL AND expires_at > now()`

type PasswordResetTokenRepository struct {
	db database.DBTX
}

func NewPasswordResetTokenRepository(db database.DBTX) *PasswordResetTokenRepository {
	return &PasswordResetTokenRepository{db: db}
}

func (r *PasswordResetTokenRepository) Create(ctx context.Context, token *domain.PasswordResetToken) error {
	return r.db.QueryRow(ctx, createPasswordResetToken, token.UserID, token.TokenDigest, token.ExpiresAt).
		Scan(&token.Id, &token.CreatedAt)
}

func (r *PasswordResetTokenRepository) FindByDigest(ctx context.Context, digest string) (*domain.PasswordResetToken, error) {
	token := &domain.PasswordResetToken{}

	err := r.db.QueryRow(ctx, findPasswordResetTokenByDigest, digest).Scan(
		&token.Id, &token.UserID, &token.TokenDigest, &token.ExpiresAt, &token.UsedAt, &token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPasswordResetTokenInvalid
		}

		return nil, err
	}

	return token, nil
}

func (r *PasswordResetTokenRepository) MarkUsed(ctx context.Context, id pgtype.UUID) error {
	_, err := r.db.Exec(ctx, markPasswordResetTokenUsed, id)

	return err
}

func (r *PasswordResetTokenRepository) InvalidateActiveForUser(ctx context.Context, userID pgtype.UUID) error {
	_, err := r.db.Exec(ctx, invalidateActivePasswordResetTokensForUser, userID)

	return err
}

var _ domain.PasswordResetTokenRepository = (*PasswordResetTokenRepository)(nil)
