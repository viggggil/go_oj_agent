USE oj_contest;
-- Stop result consumers and cache relays first. This discards only derived
-- summaries and delivery state, never submission facts or problem results.
DROP TABLE IF EXISTS contest_cache_outbox;
DROP TABLE IF EXISTS contest_user_results;
