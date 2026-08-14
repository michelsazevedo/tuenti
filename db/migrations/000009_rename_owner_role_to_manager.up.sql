-- rename the owner role to manager
ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;

UPDATE memberships SET role = 'manager' WHERE role = 'owner';

ALTER TABLE memberships ADD CONSTRAINT memberships_role_check CHECK (role IN ('manager', 'admin', 'member'));
