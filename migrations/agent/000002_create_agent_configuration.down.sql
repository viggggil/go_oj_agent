USE oj_agent;

ALTER TABLE agent_conversations DROP COLUMN agent_key;
DROP TABLE agent_config_audits;
DROP TABLE agent_config_links;
ALTER TABLE agent_config_resources DROP FOREIGN KEY fk_agent_config_current;
DROP TABLE agent_config_versions;
DROP TABLE agent_config_resources;
