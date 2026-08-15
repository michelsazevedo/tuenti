-- invitations
CREATE TABLE invitations (
    id                       UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id          UUID NOT NULL REFERENCES organizations(id),
    email                    TEXT NOT NULL,
    role                     TEXT NOT NULL CHECK (role IN ('manager', 'admin', 'member')),
    token_digest             TEXT NOT NULL,
    invited_by_membership_id UUID NOT NULL REFERENCES memberships(id),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at               TIMESTAMPTZ NOT NULL,
    accepted_at              TIMESTAMPTZ,
    revoked_at               TIMESTAMPTZ
);

-- invitation lookup indexes
CREATE UNIQUE INDEX invitations_token_digest_idx ON invitations(token_digest);
CREATE INDEX invitations_organization_id_email_idx ON invitations(organization_id, email);

CREATE UNIQUE INDEX invitations_pending_organization_id_email_idx
    ON invitations(organization_id, lower(email))
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
