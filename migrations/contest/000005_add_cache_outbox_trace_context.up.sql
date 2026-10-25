USE oj_contest;

ALTER TABLE contest_cache_outbox
  ADD COLUMN traceparent VARCHAR(255) NULL AFTER result_version,
  ADD COLUMN tracestate VARCHAR(512) NULL AFTER traceparent;
