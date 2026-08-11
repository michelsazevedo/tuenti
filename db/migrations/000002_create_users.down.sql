-- membership lookup indexes
DROP INDEX IF EXISTS idx_memberships_organization_id;
DROP INDEX IF EXISTS idx_memberships_user_id;

-- memberships
DROP TABLE IF EXISTS memberships;

-- organizations
DROP TABLE IF EXISTS organizations;

-- users
DROP TABLE IF EXISTS users;

-- citext extension
DROP EXTENSION IF EXISTS citext;
