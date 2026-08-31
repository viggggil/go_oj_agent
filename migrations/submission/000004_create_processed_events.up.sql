USE oj_submission;

CREATE TABLE processed_events (
  consumer_name VARCHAR(128) NOT NULL,
  event_id CHAR(36) NOT NULL,
  processed_at DATETIME(3) NOT NULL,
  PRIMARY KEY (consumer_name, event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

