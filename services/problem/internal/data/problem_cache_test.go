package data

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
	"time"
)

func TestRedisProblemCacheRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	cache := &RedisProblemCache{client: client, namespace: "test", ttl: time.Hour}
	ctx := context.Background()
	if err := cache.Set(ctx, biz.Problem{ID: 3, Title: "A+B"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := cache.Get(ctx, 3)
	if err != nil || !found || got.Title != "A+B" {
		t.Fatalf("got=%+v found=%v err=%v", got, found, err)
	}
	if err := cache.Delete(ctx, 3); err != nil {
		t.Fatal(err)
	}
	_, found, _ = cache.Get(ctx, 3)
	if found {
		t.Fatal("cache entry was not deleted")
	}
}
