USE oj_contest;

CREATE TABLE contest_processed_events (
 consumer_name VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 processed_at DATETIME(6) NOT NULL,
 PRIMARY KEY (consumer_name, event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE contest_submission_results (
 submission_id BIGINT NOT NULL,
 contest_id BIGINT NOT NULL,
 user_id BIGINT NOT NULL,
 problem_id BIGINT NOT NULL,
 verdict VARCHAR(32) NOT NULL,
 submitted_at DATETIME(6) NOT NULL,
 judged_at DATETIME(6) NOT NULL,
 invalidated BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY (submission_id),
 KEY idx_submission_rebuild (contest_id, user_id, problem_id, submitted_at, submission_id),
 CONSTRAINT fk_result_participant FOREIGN KEY (contest_id, user_id) REFERENCES contest_participants(contest_id, user_id),
 CONSTRAINT fk_result_problem FOREIGN KEY (contest_id, problem_id) REFERENCES contest_problems(contest_id, problem_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE contest_problem_results (
 contest_id BIGINT NOT NULL,
 user_id BIGINT NOT NULL,
 problem_id BIGINT NOT NULL,
 solved BOOLEAN NOT NULL DEFAULT FALSE,
 wrong_attempts INT NOT NULL DEFAULT 0,
 accepted_submission_id BIGINT NULL,
 accepted_at DATETIME(6) NULL,
 penalty_seconds BIGINT NOT NULL DEFAULT 0,
 updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY (contest_id, user_id, problem_id),
 CONSTRAINT fk_problem_result_participant FOREIGN KEY (contest_id, user_id) REFERENCES contest_participants(contest_id, user_id),
 CONSTRAINT fk_problem_result_problem FOREIGN KEY (contest_id, problem_id) REFERENCES contest_problems(contest_id, problem_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
