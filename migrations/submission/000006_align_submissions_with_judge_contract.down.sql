USE oj_submission;

-- Rollback also requires an empty table because MinIO source objects and
-- judge_revision cannot be converted back into the removed legacy fields.
ALTER TABLE submissions
  ADD COLUMN source_code MEDIUMTEXT NOT NULL AFTER language,
  ADD COLUMN testcase_version INT NOT NULL AFTER memory_kb,
  ADD CONSTRAINT chk_submissions_rollback_source_code_nonempty
    CHECK (CHAR_LENGTH(source_code) > 0),
  ADD CONSTRAINT chk_submissions_rollback_testcase_version
    CHECK (testcase_version > 0);

ALTER TABLE submissions
  DROP KEY idx_submissions_status_judge_deadline_at_id,
  DROP CHECK chk_submissions_source_object_key_nonempty,
  DROP CHECK chk_submissions_source_sha256,
  DROP CHECK chk_submissions_source_size_bytes,
  DROP CHECK chk_submissions_judge_revision,
  DROP CHECK chk_submissions_retry_count,
  DROP COLUMN source_object_key,
  DROP COLUMN source_sha256,
  DROP COLUMN source_size_bytes,
  DROP COLUMN judge_revision,
  DROP COLUMN retry_count,
  DROP COLUMN system_error_reason,
  DROP COLUMN judge_deadline_at,
  DROP COLUMN invalidated_at;

ALTER TABLE submissions
  DROP CHECK chk_submissions_rollback_source_code_nonempty,
  DROP CHECK chk_submissions_rollback_testcase_version;
