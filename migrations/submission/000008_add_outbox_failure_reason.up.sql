USE oj_submission;

ALTER TABLE outbox_events
  ADD COLUMN last_error VARCHAR(255) NULL AFTER lease_until;
