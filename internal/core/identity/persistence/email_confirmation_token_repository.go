package persistence

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createEmailConfirmationToken = `INSERT INTO email_confirmation_tokens(user_id, token_digest, expires_at) VALUES($1, $2, $3) RETURNING id, created_at`

const findEmailConfirmationTokenByDigest = `SELECT id, user_id, token_digest, expires_at, used_at, created_at FROM email_confirmation_tokens WHERE token_digest = $1`

const markEmailConfirmationTokenUsed = `UPDATE email_confirmation_tokens SET used_at = now() WHERE id = $1 AND used_at IS NULL`

const invalidateActiveEmailConfirmationTokensForUser = `UPDATE email_confirmation_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL AND expires_at > now()`

type EmailConfirmationTokenRepository struct {
	db database.DBTX
}

func NewEmailConfirmationTokenRepository(db database.DBTX) *EmailConfirmationTokenRepository {
	return &EmailConfirmationTokenRepository{db: db}
}

func (r *EmailConfirmationTokenRepository) Create(ctx context.Context, token *domain.EmailConfirmationToken) error {
	return r.db.QueryRow(ctx, createEmailConfirmationToken, token.UserID, token.TokenDigest, token.ExpiresAt).
		Scan(&token.Id, &token.CreatedAt)
}

func (r *EmailConfirmationTokenRepository) FindByDigest(ctx context.Context, digest string) (*domain.EmailConfirmationToken, error) {
	token := &domain.EmailConfirmationToken{}

	err := r.db.QueryRow(ctx, findEmailConfirmationTokenByDigest, digest).Scan(
		&token.Id, &token.UserID, &token.TokenDigest, &token.ExpiresAt, &token.UsedAt, &token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrEmailConfirmationTokenInvalid
		}

		return nil, err
	}

	return token, nil
}

func (r *EmailConfirmationTokenRepository) MarkUsed(ctx context.Context, id pgtype.UUID) error {
	tag, err := r.db.Exec(ctx, markEmailConfirmationTokenUsed, id)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrEmailConfirmationTokenUsed
	}

	return nil
}

func (r *EmailConfirmationTokenRepository) InvalidateActiveForUser(ctx context.Context, userID pgtype.UUID) error {
	_, err := r.db.Exec(ctx, invalidateActiveEmailConfirmationTokensForUser, userID)

	return err
}

var _ domain.EmailConfirmationTokenRepository = (*EmailConfirmationTokenRepository)(nil)
