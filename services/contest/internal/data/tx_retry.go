package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

const (
	maxTransactionAttempts = 3
	transactionRetryDelay  = 25 * time.Millisecond
)

// withContestTx retries only MySQL deadlocks and lock wait timeouts. The
// callback must be safe to run again because each attempt has a new tx.
func withContestTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	var lastErr error
	for attempt := 0; attempt < maxTransactionAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			lastErr = err
			if isRetryableTxError(err) && attempt+1 < maxTransactionAttempts {
				if err := waitTransactionRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return err
		}
		err = fn(tx)
		if err != nil {
			lastErr = err
			_ = tx.Rollback()
			if isRetryableTxError(err) && attempt+1 < maxTransactionAttempts {
				if err := waitTransactionRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if err := tx.Commit(); err != nil {
			lastErr = err
			if isRetryableTxError(err) && attempt+1 < maxTransactionAttempts {
				if err := waitTransactionRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}

func waitTransactionRetry(ctx context.Context, attempt int) error {
	delay := transactionRetryDelay << attempt
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableTxError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1205 || mysqlErr.Number == 1213
	}
	return false
}
