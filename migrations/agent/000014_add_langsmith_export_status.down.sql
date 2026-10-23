USE oj_agent;

ALTER TABLE agent_runs
  DROP CHECK chk_agent_runs_langsmith_export_status,
  DROP COLUMN langsmith_project_id,
  DROP COLUMN langsmith_export_status;
