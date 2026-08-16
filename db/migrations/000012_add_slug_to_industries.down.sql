-- industry slug lookup index
DROP INDEX IF EXISTS industries_slug_idx;

-- slug column
ALTER TABLE industries DROP COLUMN IF EXISTS slug;
