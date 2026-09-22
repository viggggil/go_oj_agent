USE oj_submission;

CREATE TABLE processed_events (
  event_id CHAR(36) NOT NULL,
  event_type VARCHAR(128) NOT NULL,
  processed_at DATETIME(3) NOT NULL,
  PRIMARY KEY (event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
