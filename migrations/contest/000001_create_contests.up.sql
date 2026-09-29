USE oj_contest;

CREATE TABLE contests (
  id BIGINT NOT NULL AUTO_INCREMENT,
  title VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL,
  start_at DATETIME(3) NOT NULL,
  end_at DATETIME(3) NOT NULL,
  created_by BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_contests_status_start_id (status, start_at, id),
  KEY idx_contests_created_by_created_at (created_by, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE contest_problems (
  contest_id BIGINT NOT NULL,
  problem_id BIGINT NOT NULL,
  sort_order INT NOT NULL,
  score INT NOT NULL DEFAULT 0,
  PRIMARY KEY (contest_id, problem_id),
  UNIQUE KEY uk_contest_problems_order (contest_id, sort_order),
  CONSTRAINT fk_contest_problems_contest FOREIGN KEY (contest_id) REFERENCES contests(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE contest_participants (
  contest_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  joined_at DATETIME(3) NOT NULL,
  PRIMARY KEY (contest_id, user_id),
  CONSTRAINT fk_contest_participants_contest FOREIGN KEY (contest_id) REFERENCES contests(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE contest_scores (
  contest_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  score INT NOT NULL DEFAULT 0,
  penalty BIGINT NOT NULL DEFAULT 0,
  accepted_count INT NOT NULL DEFAULT 0,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (contest_id, user_id),
  KEY idx_contest_scores_rank (contest_id, score, penalty),
  CONSTRAINT fk_contest_scores_contest FOREIGN KEY (contest_id) REFERENCES contests(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
