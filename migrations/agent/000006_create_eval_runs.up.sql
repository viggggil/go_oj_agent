USE oj_agent;

ALTER TABLE agent_conversations
  ADD COLUMN purpose VARCHAR(16) NOT NULL DEFAULT 'chat',
  ADD CONSTRAINT chk_agent_conversation_purpose CHECK(purpose IN ('chat','eval')),
  ADD KEY idx_agent_conversation_purpose_page(user_id, purpose, updated_at, id);

CREATE TABLE agent_eval_runs (
  id CHAR(36) NOT NULL PRIMARY KEY,
  dataset_id CHAR(36) NOT NULL,
  agent_key VARCHAR(64) NOT NULL,
  mode VARCHAR(32) NOT NULL,
  repeat_count INT NOT NULL,
  concurrency INT NOT NULL,
  status VARCHAR(16) NOT NULL,
  total_cases INT NOT NULL,
  completed_cases INT NOT NULL DEFAULT 0,
  hard_failures INT NOT NULL DEFAULT 0,
  error_count INT NOT NULL DEFAULT 0,
  config_snapshot JSON NOT NULL,
  created_by BIGINT NOT NULL,
  request_id VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  cancel_request_id CHAR(36) NULL,
  cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
  created_at DATETIME(3) NOT NULL,
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  version INT NOT NULL DEFAULT 1,
  UNIQUE KEY uk_agent_eval_runs_request (request_id),
  KEY idx_agent_eval_runs_page(created_at, id),
  CONSTRAINT fk_eval_run_dataset FOREIGN KEY(dataset_id)
    REFERENCES agent_eval_datasets(id) ON DELETE RESTRICT,
  CONSTRAINT chk_eval_run_mode CHECK(mode = 'programmatic'),
  CONSTRAINT chk_eval_run_status CHECK(status IN ('QUEUED','RUNNING','COMPLETED','FAILED','CANCELLED')),
  CONSTRAINT chk_eval_run_repeat CHECK(repeat_count BETWEEN 1 AND 3),
  CONSTRAINT chk_eval_run_concurrency CHECK(concurrency BETWEEN 1 AND 8),
  CONSTRAINT chk_eval_run_counts CHECK(total_cases > 0 AND completed_cases BETWEEN 0 AND total_cases)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE agent_eval_results (
  id CHAR(36) NOT NULL,
  run_id CHAR(36) NOT NULL,
  case_id VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  attempt INT NOT NULL,
  status VARCHAR(16) NOT NULL,
  answer MEDIUMTEXT NULL,
  checks JSON NOT NULL,
  model_summary JSON NULL,
  error_code VARCHAR(64) NULL,
  latency_ms INT NULL,
  created_at DATETIME(3) NOT NULL,
  finished_at DATETIME(3) NULL,
  PRIMARY KEY(id),
  UNIQUE KEY uk_agent_eval_result_case (run_id, case_id, attempt),
  KEY idx_agent_eval_result_page (run_id, case_id, attempt),
  CONSTRAINT fk_eval_result_run FOREIGN KEY(run_id)
    REFERENCES agent_eval_runs(id) ON DELETE CASCADE,
  CONSTRAINT chk_eval_result_status CHECK(status IN ('RUNNING','COMPLETED','FAILED','CANCELLED')),
  CONSTRAINT chk_eval_result_attempt CHECK(attempt BETWEEN 1 AND 3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
