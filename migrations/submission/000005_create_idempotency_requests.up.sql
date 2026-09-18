USE oj_submission;

CREATE TABLE idempotency_requests (
  actor_id BIGINT NOT NULL,
  operation VARCHAR(64) NOT NULL,
  idempotency_key CHAR(36) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  response JSON NULL,
  created_at DATETIME(3) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  PRIMARY KEY (actor_id, operation, idempotency_key),
  KEY idx_idempotency_requests_expires_at (expires_at),
  CONSTRAINT chk_idempotency_requests_operation_nonempty
    CHECK (CHAR_LENGTH(operation) > 0),
  CONSTRAINT chk_idempotency_requests_key_uuid_length
    CHECK (idempotency_key REGEXP '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
  CONSTRAINT chk_idempotency_requests_hash_length
    CHECK (request_hash REGEXP '^[0-9a-f]{64}$'),
  CONSTRAINT chk_idempotency_requests_expiry
    CHECK (expires_at > created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
