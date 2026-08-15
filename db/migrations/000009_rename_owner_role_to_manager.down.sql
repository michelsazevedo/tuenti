-- rename the owner role to manager
ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;

UPDATE memberships SET role = 'owner' WHERE role = 'manager';

ALTER TABLE memberships ADD CONSTRAINT memberships_role_check CHECK (role IN ('owner', 'admin', 'member'));
