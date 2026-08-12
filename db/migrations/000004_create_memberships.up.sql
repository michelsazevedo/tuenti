-- memberships
CREATE TABLE memberships (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    user_id         UUID NOT NULL REFERENCES users(id),
    role            TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT memberships_org_user_unique UNIQUE (organization_id, user_id)
);

-- membership lookup indexes
CREATE INDEX memberships_organization_id_idx ON memberships(organization_id);
CREATE INDEX memberships_user_id_idx ON memberships(user_id);
