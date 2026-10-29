-- customers
CREATE TABLE customers (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    name            VARCHAR(255) NOT NULL,
    company_name    VARCHAR(255) NOT NULL,
    email           VARCHAR(255) NOT NULL,
    phone_number    VARCHAR(50) NOT NULL,
    street          VARCHAR(255),
    complement      VARCHAR(255),
    city            VARCHAR(255),
    state           VARCHAR(255),
    country         VARCHAR(2),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

-- one active customer per email within an organization (case-insensitive, soft-delete aware)
CREATE UNIQUE INDEX customers_organization_id_email_idx ON customers(organization_id, lower(email)) WHERE deleted_at IS NULL;

-- customer lookup index (tenant-scoped listing, soft-delete aware)
CREATE INDEX customers_organization_id_deleted_at_idx ON customers(organization_id, deleted_at);
