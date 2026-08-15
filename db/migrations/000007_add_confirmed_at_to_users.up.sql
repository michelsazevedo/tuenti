-- users email confirmation column
ALTER TABLE users ADD COLUMN confirmed_at TIMESTAMPTZ;
