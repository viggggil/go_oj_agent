package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/pkg/judge"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
)

const testRevision = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

type fakeReader struct {
	objects  map[string][]byte
	err      error
	requests []string
}

func (r *fakeReader) Get(_ context.Context, bucket, key string) ([]byte, error) {
	r.requests = append(r.requests, bucket+"/"+key)
	if r.err != nil {
		return nil, r.err
	}
	value, ok := r.objects[bucket+"/"+key]
	if !ok {
		return nil, fmt.Errorf("missing object")
	}
	return value, nil
}

func TestLoaderLoad(t *testing.T) {
	task, manifest, objects := fixture(t)
	reader := &fakeReader{objects: objects}
	loader, err := NewLoader(reader, "submission-source", "problem-data")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.Source) != "package main" || len(loaded.Cases) != 1 || string(loaded.Cases[0].Input) != "1\n" || string(loaded.Cases[0].Expected) != "2\n" {
		t.Fatalf("unexpected loaded task: %+v", loaded)
	}
	manifestKey, _ := judge.ManifestObjectKey(task.ProblemID, task.JudgeRevision)
	want := []string{"submission-source/" + task.SourceObjectKey, "problem-data/" + manifestKey, "problem-data/" + manifest.Testcases[0].Input.ObjectKey, "problem-data/" + manifest.Testcases[0].Output.ObjectKey}
	if fmt.Sprint(reader.requests) != fmt.Sprint(want) {
		t.Fatalf("requests = %v, want %v", reader.requests, want)
	}
}

func TestLoaderRejectsUntrustedOrCorruptInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*mq.JudgeTask, *judge.Manifest, map[string][]byte)
		reason string
	}{
		{"source key", func(task *mq.JudgeTask, _ *judge.Manifest, _ map[string][]byte) {
			task.SourceObjectKey = "sources/../source.go"
		}, "SOURCE_INTEGRITY_FAILED"},
		{"source digest", func(task *mq.JudgeTask, _ *judge.Manifest, _ map[string][]byte) {
			task.SourceSHA256 = digest([]byte("different"))
		}, "SOURCE_INTEGRITY_FAILED"},
		{"testcase key", func(_ *mq.JudgeTask, manifest *judge.Manifest, _ map[string][]byte) {
			manifest.Testcases[0].Input.ObjectKey += ".evil"
		}, "INVALID_JUDGE_MANIFEST"},
		{"testcase digest", func(_ *mq.JudgeTask, manifest *judge.Manifest, _ map[string][]byte) {
			manifest.Testcases[0].Output.SHA256 = digest([]byte("different"))
		}, "TESTCASE_INTEGRITY_FAILED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task, manifest, objects := fixture(t)
			test.mutate(&task, &manifest, objects)
			manifestBytes, _ := json.Marshal(manifest)
			key, _ := judge.ManifestObjectKey(task.ProblemID, task.JudgeRevision)
			objects["problem-data/"+key] = manifestBytes
			loader, _ := NewLoader(&fakeReader{objects: objects}, "submission-source", "problem-data")
			_, err := loader.Load(context.Background(), task)
			reason, retryable := biz.ClassifySystemError(err)
			if reason != test.reason || retryable {
				t.Fatalf("error = %v (%s, %v)", err, reason, retryable)
			}
		})
	}
}

func TestLoaderClassifiesReaderFailureAsRetryable(t *testing.T) {
	task, _, _ := fixture(t)
	loader, _ := NewLoader(&fakeReader{err: errors.New("minio unavailable")}, "submission-source", "problem-data")
	_, err := loader.Load(context.Background(), task)
	reason, retryable := biz.ClassifySystemError(err)
	if reason != "JUDGE_INPUT_STORE_UNAVAILABLE" || !retryable {
		t.Fatalf("error = %v (%s, %v)", err, reason, retryable)
	}
}

func fixture(t *testing.T) (mq.JudgeTask, judge.Manifest, map[string][]byte) {
	t.Helper()
	source, input, output := []byte("package main"), []byte("1\n"), []byte("2\n")
	sourceKey := "sources/01ARZ3NDEKTSV4RRFFQ69G5FAV/source.go"
	prefix := "problem-7/judge-revisions/" + testRevision + "/testcases/"
	manifest := judge.Manifest{ManifestVersion: judge.ManifestVersion, ProblemID: 7, JudgeRevision: testRevision, TimeLimitMS: 1000, MemoryLimitKB: 65536, Testcases: []judge.Testcase{{CaseNo: 1, Input: judge.Object{ObjectKey: prefix + "1.in", SHA256: digest(input), SizeBytes: int64(len(input))}, Output: judge.Object{ObjectKey: prefix + "1.out", SHA256: digest(output), SizeBytes: int64(len(output))}}}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestKey, _ := judge.ManifestObjectKey(7, testRevision)
	task := mq.JudgeTask{SubmissionID: 1, ProblemID: 7, Language: "go", JudgeRevision: testRevision, SourceObjectKey: sourceKey, SourceSHA256: digest(source), SourceSizeBytes: int64(len(source)), JudgeDeadlineAt: time.Now().Add(time.Minute)}
	objects := map[string][]byte{"submission-source/" + sourceKey: source, "problem-data/" + manifestKey: manifestBytes, "problem-data/" + prefix + "1.in": input, "problem-data/" + prefix + "1.out": output}
	return task, manifest, objects
}

func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
