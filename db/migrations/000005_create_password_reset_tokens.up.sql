-- password_reset_tokens
CREATE TABLE password_reset_tokens (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id      UUID NOT NULL REFERENCES users(id),
    token_digest TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- password reset token lookup indexes
CREATE UNIQUE INDEX password_reset_tokens_token_digest_idx ON password_reset_tokens(token_digest);
CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens(user_id);
