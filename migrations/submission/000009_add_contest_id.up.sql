USE oj_submission;

ALTER TABLE submissions
  ADD COLUMN contest_id BIGINT NULL AFTER problem_id,
  ADD KEY idx_submissions_contest_user_created (contest_id, user_id, created_at, id),
  ADD KEY idx_submissions_contest_problem_created (contest_id, problem_id, created_at, id);
