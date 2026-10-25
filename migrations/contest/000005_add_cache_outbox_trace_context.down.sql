USE oj_contest;

ALTER TABLE contest_cache_outbox
  DROP COLUMN tracestate,
  DROP COLUMN traceparent;
