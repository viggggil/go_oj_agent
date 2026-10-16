USE oj_agent;

CREATE TABLE agent_eval_datasets (
  id CHAR(36) NOT NULL PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  description VARCHAR(512) NOT NULL,
  format_version VARCHAR(16) NOT NULL,
  content_hash CHAR(64) NOT NULL UNIQUE,
  object_key VARCHAR(255) NOT NULL,
  case_count INT NOT NULL,
  created_by BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  status VARCHAR(16) NOT NULL,
  CONSTRAINT chk_eval_dataset_cases CHECK(case_count > 0),
  CONSTRAINT chk_eval_dataset_status CHECK(status IN ('READY','ARCHIVED')),
  INDEX idx_eval_dataset_page(created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE agent_eval_cases (
  dataset_id CHAR(36) NOT NULL,
  case_id VARCHAR(128) NOT NULL,
  category VARCHAR(64) NOT NULL,
  title VARCHAR(256) NOT NULL,
  case_json JSON NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY(dataset_id, case_id),
  CONSTRAINT fk_eval_case_dataset FOREIGN KEY(dataset_id)
    REFERENCES agent_eval_datasets(id) ON DELETE CASCADE,
  INDEX idx_eval_case_category(dataset_id, category, case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
