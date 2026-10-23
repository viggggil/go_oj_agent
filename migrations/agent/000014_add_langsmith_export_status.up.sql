USE oj_agent;

ALTER TABLE agent_runs
  ADD COLUMN langsmith_export_status VARCHAR(16) NULL AFTER trace_id,
  ADD COLUMN langsmith_project_id CHAR(36) NULL AFTER langsmith_export_status,
  ADD CONSTRAINT chk_agent_runs_langsmith_export_status
    CHECK (langsmith_export_status IS NULL OR langsmith_export_status IN ('PENDING', 'EXPORTED', 'FAILED'));
