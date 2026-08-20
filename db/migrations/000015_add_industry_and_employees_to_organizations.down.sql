-- organizations industry & headcount columns
ALTER TABLE organizations
    DROP COLUMN IF EXISTS number_of_employees,
    DROP COLUMN IF EXISTS industry_id;
