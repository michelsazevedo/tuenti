-- items
CREATE TABLE items (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id   UUID NOT NULL REFERENCES organizations(id),
    name              VARCHAR(255) NOT NULL,
    description       TEXT,
    type              VARCHAR(20) NOT NULL,
    rate              NUMERIC(18,2) NOT NULL,
    currency          VARCHAR(10) NOT NULL,
    income_account    VARCHAR(100) NOT NULL,
    track_inventory   BOOLEAN NOT NULL DEFAULT FALSE,
    quantity_in_stock INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ,
    CONSTRAINT items_type_check CHECK (type IN ('ITEM', 'SERVICE'))
);

-- item lookup index (tenant-scoped listing, soft-delete aware)
CREATE INDEX items_organization_id_deleted_at_idx ON items(organization_id, deleted_at);
