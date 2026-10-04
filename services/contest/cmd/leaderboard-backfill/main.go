package main

import (
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("CONTEST_MYSQL_DSN")
	if dsn == "" {
		slog.Error("CONTEST_MYSQL_DSN is required; pause result consumers before running")
		os.Exit(1)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		slog.Error("open database failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	count, err := data.NewRepository(db).BackfillLeaderboard(ctx)
	if err != nil {
		slog.Error("backfill failed; safe to rerun", "completed_users", count, "error", err)
		os.Exit(1)
	}
	slog.Info("leaderboard backfill completed", "initialized_users", count)
}
