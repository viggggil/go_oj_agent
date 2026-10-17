USE oj_agent;

ALTER TABLE agent_eval_results
  ADD COLUMN review_version INT NOT NULL DEFAULT 0,
  ADD COLUMN manual_review JSON NULL,
  ADD CONSTRAINT chk_eval_review_version CHECK(review_version >= 0);

CREATE TABLE agent_eval_reviews (
  id CHAR(36) NOT NULL PRIMARY KEY,
  result_id CHAR(36) NOT NULL,
  version INT NOT NULL,
  reviewed_by BIGINT NOT NULL,
  request_id CHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  old_value JSON NULL,
  new_value JSON NOT NULL,
  reason VARCHAR(512) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_eval_review_request(request_id),
  UNIQUE KEY uk_eval_review_version(result_id, version),
  CONSTRAINT fk_eval_review_result FOREIGN KEY(result_id)
    REFERENCES agent_eval_results(id) ON DELETE CASCADE,
  CONSTRAINT chk_eval_review_audit_version CHECK(version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
