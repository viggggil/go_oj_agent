USE oj_submission;

CREATE TABLE submissions (
  id BIGINT NOT NULL AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  problem_id BIGINT NOT NULL,
  language VARCHAR(32) NOT NULL,
  source_code MEDIUMTEXT NOT NULL,
  status VARCHAR(32) NOT NULL,
  verdict VARCHAR(32) NULL,
  time_ms INT NULL,
  memory_kb INT NULL,
  testcase_version INT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  judged_at DATETIME(3) NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_submissions_user_id_created_at_id (user_id, created_at, id),
  KEY idx_submissions_problem_id_created_at_id (problem_id, created_at, id),
  KEY idx_submissions_status_created_at (status, created_at),
  KEY idx_submissions_verdict_created_at (verdict, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

