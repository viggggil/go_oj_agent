USE oj_agent;

DROP TABLE agent_eval_reviews;
ALTER TABLE agent_eval_results
  DROP CHECK chk_eval_review_version,
  DROP COLUMN manual_review,
  DROP COLUMN review_version;
