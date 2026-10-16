USE oj_agent;
DROP TABLE IF EXISTS agent_eval_results;
DROP TABLE IF EXISTS agent_eval_runs;

-- 不能在移除用途隔离后把 Eval 内容暴露给普通历史接口。
DELETE r FROM agent_runs r INNER JOIN agent_conversations c ON c.id=r.conversation_id
WHERE c.purpose='eval';
DELETE FROM agent_conversations WHERE purpose='eval';
ALTER TABLE agent_conversations
  DROP CHECK chk_agent_conversation_purpose,
  DROP INDEX idx_agent_conversation_purpose_page,
  DROP COLUMN purpose;
