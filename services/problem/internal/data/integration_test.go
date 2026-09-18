package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
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

	admin := &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}
	usecase := biz.NewProblemUsecaseWithStore(store, store, objects, store)
	testcase, err := usecase.AddTestcase(ctx, admin, created.ID, 1, []byte("1 2\n"), []byte("3\n"))
	if err != nil {
		t.Fatal(err)
	}
	var activeRevision string
	if err = db.QueryRowContext(ctx, `SELECT active_judge_revision FROM problems WHERE id = ?`, created.ID).Scan(&activeRevision); err != nil {
		t.Fatal(err)
	}
	if len(activeRevision) != 26 {
		t.Fatalf("active judge revision = %q", activeRevision)
	}
	revisionPrefix := fmt.Sprintf("problem-%d/judge-revisions/%s/", created.ID, activeRevision)
	t.Cleanup(func() {
		for object := range objects.client.ListObjects(context.Background(), objects.bucket, minio.ListObjectsOptions{Prefix: fmt.Sprintf("problem-%d/judge-revisions/", created.ID), Recursive: true}) {
			if object.Err == nil {
				_ = objects.Delete(context.Background(), object.Key)
			}
		}
	})
	manifestBytes, err := objects.Get(ctx, revisionPrefix+"manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ProblemID     int64  `json:"problem_id"`
		JudgeRevision string `json:"judge_revision"`
		Testcases     []struct {
			CaseNo int32 `json:"case_no"`
		} `json:"testcases"`
	}
	if err = json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ProblemID != created.ID || manifest.JudgeRevision != activeRevision || len(manifest.Testcases) != 1 || manifest.Testcases[0].CaseNo != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	profile, err := usecase.GetJudgeProfile(ctx, created.ID)
	if err != nil || profile.ActiveJudgeRevision != activeRevision {
		t.Fatalf("GetJudgeProfile() = %+v, %v", profile, err)
	}
	t.Cleanup(func() {
		_ = objects.Delete(context.Background(), testcase.InputObjectKey)
		_ = objects.Delete(context.Background(), testcase.OutputObjectKey)
	})
	testcase2, err := usecase.AddTestcase(ctx, admin, created.ID, 2, []byte("2 3\n"), []byte("5\n"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = objects.Delete(context.Background(), testcase2.InputObjectKey)
		_ = objects.Delete(context.Background(), testcase2.OutputObjectKey)
	})
	if _, err = usecase.ArchiveTestcase(ctx, admin, created.ID, testcase.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = objects.client.StatObject(ctx, objects.bucket, testcase.InputObjectKey, minio.StatObjectOptions{}); err != nil {
		t.Fatalf("archive deleted object: %v", err)
	}

	failing := failingTestcaseRepository{StoreSet: store}
	failingUsecase := biz.NewProblemUsecaseWithStore(store, failing, objects, store)
	var revisionBeforeFailure string
	if err = db.QueryRowContext(ctx, `SELECT active_judge_revision FROM problems WHERE id = ?`, created.ID).Scan(&revisionBeforeFailure); err != nil {
		t.Fatal(err)
	}
	if _, err = failingUsecase.AddTestcase(ctx, admin, created.ID, 3, []byte("in"), []byte("out")); err == nil {
		t.Fatal("expected metadata failure")
	}
	var revisionAfterFailure string
	if err = db.QueryRowContext(ctx, `SELECT active_judge_revision FROM problems WHERE id = ?`, created.ID).Scan(&revisionAfterFailure); err != nil {
		t.Fatal(err)
	}
	if revisionAfterFailure != revisionBeforeFailure {
		t.Fatalf("failed commit changed active revision from %s to %s", revisionBeforeFailure, revisionAfterFailure)
	}
	for object := range objects.client.ListObjects(ctx, objects.bucket, minio.ListObjectsOptions{Prefix: fmt.Sprintf("problem-%d/testcases/3/", created.ID), Recursive: true}) {
		if object.Err != nil {
			t.Fatal(object.Err)
		}
		t.Fatalf("compensation left object %s", object.Key)
	}
	if _, err = store.Archive(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = usecase.GetJudgeProfile(ctx, created.ID); !problemv1.IsProblemErrorReasonInvalidStatus(err) {
		t.Fatalf("archived profile error = %v", err)
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

type failingTestcaseRepository struct{ *StoreSet }

func (failingTestcaseRepository) CommitAddedTestcase(context.Context, biz.Testcase, string, string) (biz.Testcase, error) {
	return biz.Testcase{}, errors.New("forced metadata failure")
}
