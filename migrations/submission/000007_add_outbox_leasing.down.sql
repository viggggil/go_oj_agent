USE oj_submission;

ALTER TABLE outbox_events
  DROP CHECK chk_outbox_events_lease_pair,
  DROP COLUMN lease_owner,
  DROP COLUMN lease_until;
