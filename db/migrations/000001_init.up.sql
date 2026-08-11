-- uuid extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- pg_trgm extension (trigram-based text search & similarity)
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- citext extension (case-insensitive text, for unique email)
CREATE EXTENSION IF NOT EXISTS citext;