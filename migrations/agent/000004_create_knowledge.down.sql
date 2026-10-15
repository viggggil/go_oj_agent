-- 破坏性回滚：删除 RAG 元数据及幂等记录；不删除 MinIO 原文或 Qdrant 点。
USE oj_agent;
DROP TABLE agent_knowledge_requests;
DROP TABLE agent_knowledge_tasks;
ALTER TABLE agent_knowledge_documents DROP FOREIGN KEY fk_knowledge_current, DROP FOREIGN KEY fk_knowledge_active;
DROP TABLE agent_knowledge_versions;
DROP TABLE agent_knowledge_documents;
DROP TABLE agent_knowledge_bases;
