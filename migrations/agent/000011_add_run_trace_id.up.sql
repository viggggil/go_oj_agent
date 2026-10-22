USE oj_agent;

ALTER TABLE agent_runs
  ADD COLUMN trace_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER request_id,
  ADD KEY idx_agent_runs_trace_started (trace_id, started_at, id);
