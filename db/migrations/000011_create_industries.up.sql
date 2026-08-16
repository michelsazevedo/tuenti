-- industries
CREATE TABLE industries (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- industry lookup index
CREATE UNIQUE INDEX industries_name_idx ON industries(name);
