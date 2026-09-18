USE oj_submission;

ALTER TABLE outbox_events
  ADD COLUMN lease_owner VARCHAR(128) NULL AFTER next_retry_at,
  ADD COLUMN lease_until DATETIME(3) NULL AFTER lease_owner,
  ADD CONSTRAINT chk_outbox_events_lease_pair
    CHECK (
      (lease_owner IS NULL AND lease_until IS NULL)
      OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL)
    );
