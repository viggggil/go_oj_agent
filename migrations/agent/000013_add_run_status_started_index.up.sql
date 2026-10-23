USE oj_agent;

ALTER TABLE agent_runs
  ADD KEY idx_agent_runs_status_started_id (status, started_at, id);
