USE oj_problem;

CREATE TABLE testcases (
  id BIGINT NOT NULL AUTO_INCREMENT,
  problem_id BIGINT NOT NULL,
  version INT NOT NULL,
  case_no INT NOT NULL,
  input_object_key VARCHAR(512) NOT NULL,
  output_object_key VARCHAR(512) NOT NULL,
  input_sha256 CHAR(64) NOT NULL,
  output_sha256 CHAR(64) NOT NULL,
  size_bytes BIGINT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_testcases_problem_version_case_no (problem_id, version, case_no),
  KEY idx_testcases_problem_id_version (problem_id, version),
  CONSTRAINT fk_testcases_problem_id
    FOREIGN KEY (problem_id) REFERENCES problems (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

