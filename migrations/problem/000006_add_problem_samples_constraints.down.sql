USE oj_problem;

ALTER TABLE problems
  DROP COLUMN samples_json,
  DROP COLUMN constraints_text;
