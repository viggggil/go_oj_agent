USE oj_submission;

-- This migration intentionally fails when legacy rows exist. Moving source_code
-- to MinIO requires an application-level data migration that creates immutable
-- objects and real hashes; SQL must not fabricate those references.
ALTER TABLE submissions
  ADD COLUMN source_object_key VARCHAR(512) NOT NULL AFTER language,
  ADD COLUMN source_sha256 CHAR(64) NOT NULL AFTER source_object_key,
  ADD COLUMN source_size_bytes BIGINT NOT NULL AFTER source_sha256,
  ADD COLUMN judge_revision CHAR(26) NOT NULL AFTER source_size_bytes,
  ADD COLUMN retry_count INT NOT NULL DEFAULT 0 AFTER memory_kb,
  ADD COLUMN system_error_reason VARCHAR(128) NULL AFTER retry_count,
  ADD COLUMN judge_deadline_at DATETIME(3) NOT NULL AFTER system_error_reason,
  ADD COLUMN invalidated_at DATETIME(3) NULL AFTER judged_at,
  ADD CONSTRAINT chk_submissions_source_object_key_nonempty
    CHECK (CHAR_LENGTH(source_object_key) > 0),
  ADD CONSTRAINT chk_submissions_source_sha256
    CHECK (source_sha256 REGEXP '^[0-9a-f]{64}$'),
  ADD CONSTRAINT chk_submissions_source_size_bytes
    CHECK (source_size_bytes > 0),
  ADD CONSTRAINT chk_submissions_judge_revision
    CHECK (CHAR_LENGTH(judge_revision) = 26),
  ADD CONSTRAINT chk_submissions_retry_count
    CHECK (retry_count BETWEEN 0 AND 3),
  ADD KEY idx_submissions_status_judge_deadline_at_id
    (status, judge_deadline_at, id);

ALTER TABLE submissions
  DROP COLUMN source_code,
  DROP COLUMN testcase_version;
