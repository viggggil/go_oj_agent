USE oj_problem;

ALTER TABLE problems
  ADD COLUMN constraints_text TEXT NOT NULL DEFAULT ('') AFTER description,
  ADD COLUMN samples_json MEDIUMTEXT NOT NULL DEFAULT ('[]') AFTER constraints_text;
