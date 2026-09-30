USE oj_submission;

ALTER TABLE submissions
  DROP KEY idx_submissions_contest_user_created,
  DROP KEY idx_submissions_contest_problem_created,
  DROP COLUMN contest_id;
