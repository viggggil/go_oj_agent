package data

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"strconv"
	"time"
)

func (r *Repository) ClaimCacheEvent(ctx context.Context, token string, lease time.Duration) (*biz.CacheOutboxEvent, error) {
	if _, err := uuid.Parse(token); err != nil || lease <= 0 || lease > time.Minute {
		return nil, fmt.Errorf("invalid cache lease")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	event := &biz.CacheOutboxEvent{}
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT id,contest_id,user_id,result_version,payload,retry_count FROM contest_cache_outbox
 WHERE status='pending' AND next_retry_at<=UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6)) ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&event.ID, &event.ContestID, &event.UserID, &version, &event.Payload, &event.Retries)
	if err == sql.ErrNoRows {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	event.Version = strconv.FormatInt(version, 10)
	_, err = tx.ExecContext(ctx, `UPDATE contest_cache_outbox SET lease_owner=?,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE id=?`, token, lease.Microseconds(), event.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return event, nil
}
func (r *Repository) CompleteCacheEvent(ctx context.Context, id int64, token string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE contest_cache_outbox SET status='applied',applied_at=UTC_TIMESTAMP(6),lease_owner=NULL,lease_until=NULL,last_error=NULL
 WHERE id=? AND status='pending' AND lease_owner=? AND lease_until>UTC_TIMESTAMP(6)`, id, token)
	return cacheLeaseResult(result, err)
}
func (r *Repository) FailCacheEvent(ctx context.Context, id int64, token string, delay time.Duration, dead bool, reason string) error {
	if delay < 0 || delay > time.Hour {
		return fmt.Errorf("invalid cache retry delay")
	}
	state := "pending"
	if dead {
		state = "dead"
	}
	if len(reason) > 255 {
		reason = reason[:255]
	}
	result, err := r.db.ExecContext(ctx, `UPDATE contest_cache_outbox SET status=?,retry_count=retry_count+1,next_retry_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),lease_owner=NULL,lease_until=NULL,last_error=?
 WHERE id=? AND status='pending' AND lease_owner=? AND lease_until>UTC_TIMESTAMP(6)`, state, delay.Microseconds(), reason, id, token)
	return cacheLeaseResult(result, err)
}
func cacheLeaseResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return biz.ErrLeaseLost
	}
	return nil
}
