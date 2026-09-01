USE oj_user;

INSERT INTO roles (name, description)
VALUES
  ('user', '普通用户'),
  ('admin', '管理员')
ON DUPLICATE KEY UPDATE
  description = VALUES(description);

