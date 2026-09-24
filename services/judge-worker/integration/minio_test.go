package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/oklog/ulid/v2"

	judgecontract "github.com/viggggil/go_oj_agent/pkg/judge"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/storage"
)

func TestMinIOLoader(t *testing.T) {
	endpoint, accessKey, secretKey := os.Getenv("MINIO_TEST_ENDPOINT"), os.Getenv("MINIO_TEST_ACCESS_KEY"), os.Getenv("MINIO_TEST_SECRET_KEY")
	if endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("set MINIO_TEST_ENDPOINT, MINIO_TEST_ACCESS_KEY and MINIO_TEST_SECRET_KEY")
	}
	ctx := t.Context()
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, "")})
	if err != nil {
		t.Fatal(err)
	}
	suffix := strings.ToLower(ulid.Make().String())
	sourceBucket, problemBucket := "worker-source-"+suffix, "worker-problem-"+suffix
	for _, bucket := range []string{sourceBucket, problemBucket} {
		if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
		bucket := bucket
		t.Cleanup(func() { _ = client.RemoveBucket(context.Background(), bucket) })
	}

	source, input, output := []byte("package main\nfunc main() {}\n"), []byte("1\n"), []byte("2\n")
	sourceID, revision := ulid.Make().String(), ulid.Make().String()
	sourceKey := "sources/" + sourceID + "/source.go"
	prefix := "problem-7/judge-revisions/" + revision + "/testcases/"
	manifest := judgecontract.Manifest{ManifestVersion: judgecontract.ManifestVersion, ProblemID: 7, JudgeRevision: revision, TimeLimitMS: 1000, MemoryLimitKB: 65536, Testcases: []judgecontract.Testcase{{CaseNo: 1, Input: object(prefix+"1.in", input), Output: object(prefix+"1.out", output)}}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestKey, err := judgecontract.ManifestObjectKey(7, revision)
	if err != nil {
		t.Fatal(err)
	}
	objects := []struct {
		bucket, key string
		body        []byte
	}{{sourceBucket, sourceKey, source}, {problemBucket, manifestKey, manifestBytes}, {problemBucket, prefix + "1.in", input}, {problemBucket, prefix + "1.out", output}}
	for _, item := range objects {
		if _, err = client.PutObject(ctx, item.bucket, item.key, bytes.NewReader(item.body), int64(len(item.body)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
		item := item
		t.Cleanup(func() {
			_ = client.RemoveObject(context.Background(), item.bucket, item.key, minio.RemoveObjectOptions{})
		})
	}

	readerConfig := minioConfig(endpoint, accessKey, secretKey)
	reader, err := storage.NewMinIOReader(readerConfig)
	if err != nil {
		t.Fatal(err)
	}
	loader, err := storage.NewLoader(reader, sourceBucket, problemBucket)
	if err != nil {
		t.Fatal(err)
	}
	task := mq.JudgeTask{SubmissionID: 1, ProblemID: 7, Language: "go", JudgeRevision: revision, SourceObjectKey: sourceKey, SourceSHA256: hash(source), SourceSizeBytes: int64(len(source)), JudgeDeadlineAt: time.Now().Add(time.Minute)}
	loaded, err := loader.Load(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Cases) != 1 || !bytes.Equal(loaded.Source, source) || !bytes.Equal(loaded.Cases[0].Expected, output) {
		t.Fatalf("unexpected loaded task: %+v", loaded)
	}
}

func object(key string, value []byte) judgecontract.Object {
	return judgecontract.Object{ObjectKey: key, SHA256: hash(value), SizeBytes: int64(len(value))}
}
func hash(value []byte) string { digest := sha256.Sum256(value); return hex.EncodeToString(digest[:]) }

// Keep config construction local so this integration test exercises the same production constructor.
func minioConfig(endpoint, accessKey, secretKey string) *conf.Bootstrap {
	return &conf.Bootstrap{Storage: conf.Storage{MinIO: conf.MinIO{Endpoint: endpoint, AccessKey: accessKey, SecretKey: secretKey}}}
}
