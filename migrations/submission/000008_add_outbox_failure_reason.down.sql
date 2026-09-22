USE oj_submission;

ALTER TABLE outbox_events
  DROP COLUMN last_error;
