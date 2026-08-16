-- slug column
ALTER TABLE industries ADD COLUMN slug TEXT;

-- backfill existing rows so the NOT NULL constraint below doesn't fail on a non-empty table
UPDATE industries SET slug = lower(regexp_replace(replace(name, '&', ''), '\s+', '_', 'g'));

ALTER TABLE industries ALTER COLUMN slug SET NOT NULL;

-- industry slug lookup index
CREATE UNIQUE INDEX industries_slug_idx ON industries(slug);
