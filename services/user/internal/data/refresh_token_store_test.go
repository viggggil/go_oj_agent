package data

import (
	"context"
	"fmt"
	"sync"
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
	nextRecord.SessionID = ""

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
	if next.SessionID != oldRecord.SessionID {
		t.Fatalf("next SessionID = %q, want %q", next.SessionID, oldRecord.SessionID)
	}
	hashes, err := store.client.SMembers(ctx, store.sessionKey(oldRecord.SessionID)).Result()
	if err != nil {
		t.Fatalf("SMembers(session) error = %v", err)
	}
	if !contains(hashes, nextRecord.TokenHash) {
		t.Fatalf("session hashes = %v, want next token hash", hashes)
	}
}

func TestRedisRefreshTokenStoreRotateAllowsOnlyOneConcurrentSuccess(t *testing.T) {
	store, cleanup := newTestRefreshTokenStore(t)
	defer cleanup()
	ctx := context.Background()

	oldRecord := testRefreshTokenRecord("old-hash")
	if err := store.Save(ctx, oldRecord); err != nil {
		t.Fatalf("Save(old) error = %v", err)
	}

	const attempts = 16
	start := make(chan struct{})
	errors := make(chan error, attempts)
	var waitGroup sync.WaitGroup
	for i := 0; i < attempts; i++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			next := testRefreshTokenRecord(fmt.Sprintf("next-hash-%d", index))
			next.TokenID = fmt.Sprintf("next-token-%d", index)
			next.SessionID = oldRecord.SessionID
			<-start
			errors <- store.Rotate(ctx, oldRecord.TokenHash, next)
		}(i)
	}
	close(start)
	waitGroup.Wait()
	close(errors)

	successes := 0
	denied := 0
	for err := range errors {
		switch err {
		case nil:
			successes++
		case biz.ErrRefreshTokenDenied:
			denied++
		default:
			t.Fatalf("Rotate() unexpected error = %v", err)
		}
	}
	if successes != 1 || denied != attempts-1 {
		t.Fatalf("Rotate() successes = %d, denied = %d, want 1 and %d", successes, denied, attempts-1)
	}

	old, err := store.FindByHash(ctx, oldRecord.TokenHash)
	if err != nil {
		t.Fatalf("FindByHash(old) error = %v", err)
	}
	if !old.Revoked || !old.ReplayLocked {
		t.Fatalf("old token state = revoked:%t replay_locked:%t, want both true", old.Revoked, old.ReplayLocked)
	}

	storedNextTokens := 0
	for i := 0; i < attempts; i++ {
		if _, err := store.FindByHash(ctx, fmt.Sprintf("next-hash-%d", i)); err == nil {
			storedNextTokens++
		}
	}
	if storedNextTokens != 1 {
		t.Fatalf("stored next tokens = %d, want 1", storedNextTokens)
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
