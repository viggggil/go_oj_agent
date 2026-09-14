package data

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"github.com/viggggil/go_oj_agent/services/problem/internal/conf"
)

func TestProblemInfrastructureIntegration(t *testing.T) {
	mysqlDSN := os.Getenv("PROBLEM_TEST_MYSQL_DSN")
	redisAddr := os.Getenv("PROBLEM_TEST_REDIS_ADDR")
	minioEndpoint := os.Getenv("PROBLEM_TEST_MINIO_ENDPOINT")
	if mysqlDSN == "" || redisAddr == "" || minioEndpoint == "" {
		t.Skip("set PROBLEM_TEST_MYSQL_DSN, PROBLEM_TEST_REDIS_ADDR and PROBLEM_TEST_MINIO_ENDPOINT")
	}
	ctx := t.Context()
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStoreSet(db)

	suffix := time.Now().UnixNano()
	created, err := store.Create(ctx, biz.Problem{Title: "Integration Problem", Slug: fmt.Sprintf("integration-%d", suffix), Description: "statement", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: 1}, []string{"integration"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DELETE FROM problems WHERE id = ?", created.ID) })
	found, err := store.FindByID(ctx, created.ID)
	if err != nil || len(found.Tags) != 1 {
		t.Fatalf("FindByID() = %+v, %v", found, err)
	}
	created.Title = "Updated Integration Problem"
	updated, err := store.Update(ctx, created, []string{"updated"})
	if err != nil || updated.Title != created.Title {
		t.Fatalf("Update() = %+v, %v", updated, err)
	}
	if _, err = store.Archive(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	config := &conf.Bootstrap{Storage: &conf.StorageProto{Minio: &conf.MinIOProto{Endpoint: minioEndpoint, AccessKey: envOr("PROBLEM_TEST_MINIO_ACCESS_KEY", "minioadmin"), SecretKey: envOr("PROBLEM_TEST_MINIO_SECRET_KEY", "minioadmin"), Bucket: "problem-data"}}}
	objects, err := NewMinIOStore(config)
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("integration/%d.in", suffix)
	if err = objects.Put(ctx, key, []byte("test input")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = objects.Delete(context.Background(), key) })
	info, err := objects.client.StatObject(ctx, objects.bucket, key, minio.StatObjectOptions{})
	if err != nil || info.Size != 10 {
		t.Fatalf("StatObject() size=%d err=%v", info.Size, err)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()
	cache := NewProblemCache(redisClient, &conf.Bootstrap{Data: &conf.DataProto{RedisNamespace: "integration"}})
	if err = cache.Set(ctx, updated); err != nil {
		t.Fatal(err)
	}
	cached, ok, err := cache.Get(ctx, updated.ID)
	if err != nil || !ok || cached.Title != updated.Title {
		t.Fatalf("cache.Get() = %+v,%v,%v", cached, ok, err)
	}
	if err = cache.Delete(ctx, updated.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = cache.Get(ctx, updated.ID); err != nil || ok {
		t.Fatalf("cache after delete ok=%v err=%v", ok, err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
