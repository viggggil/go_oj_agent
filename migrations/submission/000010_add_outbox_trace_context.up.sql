USE oj_submission;

ALTER TABLE outbox_events
  ADD COLUMN traceparent VARCHAR(255) NULL AFTER event_version,
  ADD COLUMN tracestate VARCHAR(512) NULL AFTER traceparent;
