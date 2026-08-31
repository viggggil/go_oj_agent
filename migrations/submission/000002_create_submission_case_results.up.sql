USE oj_submission;

CREATE TABLE submission_case_results (
  id BIGINT NOT NULL AUTO_INCREMENT,
  submission_id BIGINT NOT NULL,
  case_no INT NOT NULL,
  verdict VARCHAR(32) NOT NULL,
  time_ms INT NULL,
  memory_kb INT NULL,
  message VARCHAR(1024) NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_submission_case_results_submission_case_no (submission_id, case_no),
  KEY idx_submission_case_results_submission_id (submission_id),
  CONSTRAINT fk_submission_case_results_submission_id
    FOREIGN KEY (submission_id) REFERENCES submissions (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

