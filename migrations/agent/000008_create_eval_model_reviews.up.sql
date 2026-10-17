USE oj_agent;

ALTER TABLE agent_eval_runs
  MODIFY COLUMN mode VARCHAR(32) NOT NULL,
  ADD COLUMN reviewer_agent_key VARCHAR(64) NULL,
  ADD COLUMN reviewer_config_snapshot JSON NULL,
  DROP CHECK chk_eval_run_mode,
  ADD CONSTRAINT chk_eval_run_mode CHECK(mode IN ('programmatic','programmatic_review')),
  ADD CONSTRAINT chk_eval_run_reviewer CHECK(
    (mode = 'programmatic' AND reviewer_agent_key IS NULL AND reviewer_config_snapshot IS NULL)
    OR (mode = 'programmatic_review' AND reviewer_agent_key IS NOT NULL AND reviewer_config_snapshot IS NOT NULL)
  );

ALTER TABLE agent_eval_results
  ADD COLUMN model_review_status VARCHAR(16) NOT NULL DEFAULT 'NOT_REQUESTED',
  ADD COLUMN model_review_attempt INT NOT NULL DEFAULT 0,
  ADD COLUMN model_review JSON NULL,
  ADD COLUMN model_review_error_code VARCHAR(64) NULL,
  ADD CONSTRAINT chk_eval_model_review_status CHECK(model_review_status IN ('NOT_REQUESTED','PENDING','RUNNING','COMPLETED','UNAVAILABLE','SKIPPED')),
  ADD CONSTRAINT chk_eval_model_review_attempt CHECK(model_review_attempt >= 0);

CREATE TABLE agent_eval_model_reviews (
  id CHAR(36) NOT NULL PRIMARY KEY,
  result_id CHAR(36) NOT NULL,
  attempt INT NOT NULL,
  requested_by BIGINT NOT NULL,
  request_id CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL,
  review JSON NULL,
  error_code VARCHAR(64) NULL,
  created_at DATETIME(3) NOT NULL,
  finished_at DATETIME(3) NULL,
  UNIQUE KEY uk_eval_model_review_request(request_id),
  UNIQUE KEY uk_eval_model_review_attempt(result_id, attempt),
  CONSTRAINT fk_eval_model_review_result FOREIGN KEY(result_id)
    REFERENCES agent_eval_results(id) ON DELETE CASCADE,
  CONSTRAINT chk_eval_model_attempt_number CHECK(attempt > 0),
  CONSTRAINT chk_eval_model_attempt_status CHECK(status IN ('RUNNING','COMPLETED','UNAVAILABLE'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
