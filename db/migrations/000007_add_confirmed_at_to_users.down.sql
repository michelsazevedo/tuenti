-- users email confirmation column
ALTER TABLE users DROP COLUMN IF EXISTS confirmed_at;
