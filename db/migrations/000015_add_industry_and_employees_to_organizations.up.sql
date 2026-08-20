-- organizations industry & headcount columns
ALTER TABLE organizations
    ADD COLUMN industry_id          UUID NOT NULL REFERENCES industries(id),
    ADD COLUMN number_of_employees  INTEGER NOT NULL;
