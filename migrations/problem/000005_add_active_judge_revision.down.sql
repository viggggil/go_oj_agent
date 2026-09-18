USE oj_problem;

ALTER TABLE problems
  DROP CHECK chk_problems_active_judge_revision,
  DROP COLUMN active_judge_revision;
