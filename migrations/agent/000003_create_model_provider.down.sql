USE oj_agent;
-- 必须先清理新 Provider 配置及其引用；有 Provider 时由 CHECK 拒绝回滚。
-- 回滚删除加密凭据和模型调用摘要，但不删除会话、消息或 Run。
ALTER TABLE agent_config_resources DROP CHECK chk_agent_config_kind,
  ADD CONSTRAINT chk_agent_config_kind
  CHECK (kind IN ('agent', 'prompt', 'skill', 'model'));
ALTER TABLE agent_runs DROP COLUMN model_summary;
DROP TABLE agent_provider_credentials;
DROP TABLE agent_credential_audits;
DROP TABLE agent_credentials;
