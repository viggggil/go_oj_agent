USE oj_problem;

-- Historical judge revisions live only in immutable MinIO prefixes. MySQL
-- keeps the pointer that represents the latest published testcase state.
ALTER TABLE problems
  ADD COLUMN active_judge_revision CHAR(26) NULL AFTER memory_limit_kb,
  ADD CONSTRAINT chk_problems_active_judge_revision
    CHECK (active_judge_revision IS NULL OR CHAR_LENGTH(active_judge_revision) = 26);
