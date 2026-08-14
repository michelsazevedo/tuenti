-- email_confirmation_tokens
CREATE TABLE email_confirmation_tokens (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id      UUID NOT NULL REFERENCES users(id),
    token_digest TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- email confirmation token lookup indexes
CREATE UNIQUE INDEX email_confirmation_tokens_token_digest_idx ON email_confirmation_tokens(token_digest);
CREATE INDEX email_confirmation_tokens_user_id_idx ON email_confirmation_tokens(user_id);
