USE oj_agent;

ALTER TABLE agent_runs
  ADD KEY idx_agent_runs_started_id (started_at, id);
