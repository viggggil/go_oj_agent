USE oj_agent;

ALTER TABLE agent_runs
  DROP KEY idx_agent_runs_trace_started,
  DROP COLUMN trace_id;
