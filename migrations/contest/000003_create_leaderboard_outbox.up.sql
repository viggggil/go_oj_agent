USE oj_contest;

CREATE TABLE contest_user_results (
 contest_id BIGINT NOT NULL,
 user_id BIGINT NOT NULL,
 solved_count INT NOT NULL,
 penalty_seconds BIGINT NOT NULL,
 version BIGINT NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY (contest_id,user_id),
 CONSTRAINT chk_contest_user_result CHECK (solved_count BETWEEN 0 AND 100 AND penalty_seconds >= 0 AND version > 0),
 CONSTRAINT fk_user_result_participant FOREIGN KEY (contest_id,user_id) REFERENCES contest_participants(contest_id,user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE contest_cache_outbox (
 id BIGINT NOT NULL AUTO_INCREMENT,
 event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 contest_id BIGINT NOT NULL,
 user_id BIGINT NOT NULL,
 result_version BIGINT NOT NULL,
 payload JSON NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'pending',
 retry_count INT NOT NULL DEFAULT 0,
 next_retry_at DATETIME(6) NOT NULL,
 lease_owner CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
 lease_until DATETIME(6) NULL,
 last_error VARCHAR(255) NULL,
 created_at DATETIME(6) NOT NULL,
 applied_at DATETIME(6) NULL,
 PRIMARY KEY(id),
 UNIQUE KEY uk_contest_cache_event(event_id),
 UNIQUE KEY uk_contest_cache_result(contest_id,user_id,result_version),
 KEY idx_contest_cache_pending(status,next_retry_at,id),
 CONSTRAINT chk_contest_cache_state CHECK (status IN ('pending','applied','dead') AND result_version > 0 AND retry_count >= 0),
 CONSTRAINT chk_contest_cache_lease CHECK ((lease_owner IS NULL AND lease_until IS NULL) OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
