-- item taxes
CREATE TABLE item_taxes (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    item_id    UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    name       VARCHAR(255) NOT NULL,
    tax_number VARCHAR(255),
    rate       NUMERIC(5,2) NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT item_taxes_rate_check CHECK (rate >= 0 AND rate <= 100)
);

-- item tax lookup index
CREATE INDEX item_taxes_item_id_idx ON item_taxes(item_id);
