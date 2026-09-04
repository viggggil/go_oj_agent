package data

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

func TestRedisRefreshTokenStoreSaveAndFind(t *testing.T) {
	store, cleanup := newTestRefreshTokenStore(t)
	defer cleanup()

	record := testRefreshTokenRecord("hash-1")
	if err := store.Save(context.Background(), record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.FindByHash(context.Background(), "hash-1")
	if err != nil {
		t.Fatalf("FindByHash() error = %v", err)
	}
	if got.UserID != record.UserID || got.TokenHash != record.TokenHash {
		t.Fatalf("record = %#v, want %#v", got, record)
	}
}

func TestRedisRefreshTokenStoreRotateRevokesOldToken(t *testing.T) {
	store, cleanup := newTestRefreshTokenStore(t)
	defer cleanup()
	ctx := context.Background()

	oldRecord := testRefreshTokenRecord("old-hash")
	if err := store.Save(ctx, oldRecord); err != nil {
		t.Fatalf("Save(old) error = %v", err)
	}
	nextRecord := testRefreshTokenRecord("next-hash")
	nextRecord.SessionID = oldRecord.SessionID

	if err := store.Rotate(ctx, oldRecord.TokenHash, nextRecord); err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}

	old, err := store.FindByHash(ctx, oldRecord.TokenHash)
	if err != nil {
		t.Fatalf("FindByHash(old) error = %v", err)
	}
	if !old.Revoked {
		t.Fatal("old token Revoked = false, want true")
	}
	next, err := store.FindByHash(ctx, nextRecord.TokenHash)
	if err != nil {
		t.Fatalf("FindByHash(next) error = %v", err)
	}
	if next.RotatedFrom != oldRecord.TokenID {
		t.Fatalf("next RotatedFrom = %q, want %q", next.RotatedFrom, oldRecord.TokenID)
	}
}

func TestRedisRefreshTokenStoreRevokeSession(t *testing.T) {
	store, cleanup := newTestRefreshTokenStore(t)
	defer cleanup()
	ctx := context.Background()

	record := testRefreshTokenRecord("hash-1")
	if err := store.Save(ctx, record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.RevokeSession(ctx, record.SessionID); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}

	got, err := store.FindByHash(ctx, record.TokenHash)
	if err != nil {
		t.Fatalf("FindByHash() error = %v", err)
	}
	if !got.Revoked {
		t.Fatal("Revoked = false, want true")
	}
}

func newTestRefreshTokenStore(t *testing.T) (*RedisRefreshTokenStore, func()) {
	t.Helper()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	store := NewRedisRefreshTokenStore(client, "test:user", func() time.Time {
		return now
	})
	return store, func() {
		_ = client.Close()
		server.Close()
	}
}

func testRefreshTokenRecord(hash string) biz.RefreshTokenRecord {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	return biz.RefreshTokenRecord{
		UserID:    1001,
		SessionID: "session-1",
		TokenID:   "token-1",
		TokenHash: hash,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
}
