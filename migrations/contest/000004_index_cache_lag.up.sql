USE oj_contest;
ALTER TABLE contest_cache_outbox ADD KEY idx_contest_cache_lag (contest_id,status,created_at);
