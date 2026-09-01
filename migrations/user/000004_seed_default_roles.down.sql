USE oj_user;

DELETE FROM user_roles
WHERE role_id IN (
  SELECT id FROM roles WHERE name IN ('user', 'admin')
);

DELETE FROM roles
WHERE name IN ('user', 'admin');

