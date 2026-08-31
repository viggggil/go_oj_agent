USE oj_problem;

CREATE TABLE problems (
  id BIGINT NOT NULL AUTO_INCREMENT,
  title VARCHAR(255) NOT NULL,
  slug VARCHAR(255) NOT NULL,
  description TEXT NOT NULL,
  difficulty VARCHAR(32) NOT NULL,
  time_limit_ms INT NOT NULL,
  memory_limit_kb INT NOT NULL,
  status VARCHAR(32) NOT NULL,
  created_by BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_problems_slug (slug),
  KEY idx_problems_status_difficulty_id (status, difficulty, id),
  KEY idx_problems_created_by_created_at (created_by, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

