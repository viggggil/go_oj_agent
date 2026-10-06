-- 仅用于隔离的本地开发环境；应用用户仅拥有 oj_agent 数据权限。
-- 表由 migrations/agent 创建；共享/生产环境由部署者配置独立账户和凭据。
CREATE USER IF NOT EXISTS 'oj_agent'@'%' IDENTIFIED BY 'local-agent-password';
GRANT SELECT, INSERT, UPDATE, DELETE ON oj_agent.* TO 'oj_agent'@'%';
