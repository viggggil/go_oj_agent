USE oj_agent;

UPDATE agent_eval_runs
SET mode='programmatic', reviewer_agent_key=NULL, reviewer_config_snapshot=NULL;
DROP TABLE agent_eval_model_reviews;
ALTER TABLE agent_eval_results
  DROP CHECK chk_eval_model_review_attempt,
  DROP CHECK chk_eval_model_review_status,
  DROP COLUMN model_review_error_code,
  DROP COLUMN model_review,
  DROP COLUMN model_review_attempt,
  DROP COLUMN model_review_status;
ALTER TABLE agent_eval_runs
  DROP CHECK chk_eval_run_reviewer,
  DROP CHECK chk_eval_run_mode,
  DROP COLUMN reviewer_config_snapshot,
  DROP COLUMN reviewer_agent_key,
  MODIFY COLUMN mode VARCHAR(16) NOT NULL,
  ADD CONSTRAINT chk_eval_run_mode CHECK(mode = 'programmatic');
