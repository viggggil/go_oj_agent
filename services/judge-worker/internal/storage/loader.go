package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/viggggil/go_oj_agent/pkg/judge"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
)

type Loader struct {
	reader                      Reader
	sourceBucket, problemBucket string
}

const maxSourceSize = 1 << 20

func NewLoader(reader Reader, sourceBucket, problemBucket string) (*Loader, error) {
	if reader == nil || strings.TrimSpace(sourceBucket) == "" || strings.TrimSpace(problemBucket) == "" {
		return nil, fmt.Errorf("judge input loader is not configured")
	}
	return &Loader{reader: reader, sourceBucket: sourceBucket, problemBucket: problemBucket}, nil
}

func (l *Loader) Load(ctx context.Context, task mq.JudgeTask) (biz.LoadedTask, error) {
	if l == nil || l.reader == nil {
		return biz.LoadedTask{}, biz.NewSystemError("JUDGE_INPUT_STORE_UNAVAILABLE", true, nil)
	}
	if err := task.Validate(); err != nil {
		return biz.LoadedTask{}, biz.NewSystemError("INVALID_TASK_INPUT", false, err)
	}
	if err := validateSourceKey(task.SourceObjectKey, task.Language); err != nil {
		return biz.LoadedTask{}, biz.NewSystemError("SOURCE_INTEGRITY_FAILED", false, err)
	}
	if task.SourceSizeBytes > maxSourceSize {
		return biz.LoadedTask{}, biz.NewSystemError("SOURCE_INTEGRITY_FAILED", false, fmt.Errorf("source exceeds %d bytes", maxSourceSize))
	}
	source, err := l.read(ctx, l.sourceBucket, task.SourceObjectKey)
	if err != nil {
		return biz.LoadedTask{}, err
	}
	if err := verifyObject(source, task.SourceSHA256, task.SourceSizeBytes); err != nil {
		return biz.LoadedTask{}, biz.NewSystemError("SOURCE_INTEGRITY_FAILED", false, err)
	}
	manifestKey, err := judge.ManifestObjectKey(task.ProblemID, task.JudgeRevision)
	if err != nil {
		return biz.LoadedTask{}, biz.NewSystemError("INVALID_JUDGE_MANIFEST", false, err)
	}
	manifestBytes, err := l.read(ctx, l.problemBucket, manifestKey)
	if err != nil {
		return biz.LoadedTask{}, err
	}
	var manifest judge.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return biz.LoadedTask{}, biz.NewSystemError("INVALID_JUDGE_MANIFEST", false, err)
	}
	if err := manifest.Validate(); err != nil || manifest.ProblemID != task.ProblemID || manifest.JudgeRevision != task.JudgeRevision {
		return biz.LoadedTask{}, biz.NewSystemError("INVALID_JUDGE_MANIFEST", false, err)
	}
	cases := make([]biz.LoadedCase, 0, len(manifest.Testcases))
	for index, testcase := range manifest.Testcases {
		if index > 0 && manifest.Testcases[index-1].CaseNo >= testcase.CaseNo {
			return biz.LoadedTask{}, biz.NewSystemError("INVALID_JUDGE_MANIFEST", false, fmt.Errorf("testcases must be ordered by case number"))
		}
		prefix := fmt.Sprintf("problem-%d/judge-revisions/%s/testcases/", task.ProblemID, task.JudgeRevision)
		if testcase.Input.ObjectKey != prefix+fmt.Sprintf("%d.in", testcase.CaseNo) || testcase.Output.ObjectKey != prefix+fmt.Sprintf("%d.out", testcase.CaseNo) {
			return biz.LoadedTask{}, biz.NewSystemError("INVALID_JUDGE_MANIFEST", false, fmt.Errorf("testcase %d object key mismatch", testcase.CaseNo))
		}
		input, readErr := l.read(ctx, l.problemBucket, testcase.Input.ObjectKey)
		if readErr != nil {
			return biz.LoadedTask{}, readErr
		}
		if readErr = verifyObject(input, testcase.Input.SHA256, testcase.Input.SizeBytes); readErr != nil {
			return biz.LoadedTask{}, biz.NewSystemError("TESTCASE_INTEGRITY_FAILED", false, readErr)
		}
		output, readErr := l.read(ctx, l.problemBucket, testcase.Output.ObjectKey)
		if readErr != nil {
			return biz.LoadedTask{}, readErr
		}
		if readErr = verifyObject(output, testcase.Output.SHA256, testcase.Output.SizeBytes); readErr != nil {
			return biz.LoadedTask{}, biz.NewSystemError("TESTCASE_INTEGRITY_FAILED", false, readErr)
		}
		cases = append(cases, biz.LoadedCase{CaseNo: testcase.CaseNo, Input: input, Expected: output})
	}
	return biz.LoadedTask{Task: task, Source: source, Manifest: manifest, Cases: cases}, nil
}

func (l *Loader) read(ctx context.Context, bucket, key string) ([]byte, error) {
	content, err := l.reader.Get(ctx, bucket, key)
	if err == nil {
		return content, nil
	}
	var systemErr *biz.SystemError
	if errors.As(err, &systemErr) {
		return nil, err
	}
	return nil, biz.NewSystemError("JUDGE_INPUT_STORE_UNAVAILABLE", true, err)
}

func verifyObject(content []byte, expectedHash string, expectedSize int64) error {
	if int64(len(content)) != expectedSize {
		return fmt.Errorf("size mismatch: got %d want %d", len(content), expectedSize)
	}
	digest := sha256.Sum256(content)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), expectedHash) {
		return fmt.Errorf("sha256 mismatch")
	}
	return nil
}

func validateSourceKey(key, language string) error {
	if len(key) > 512 || path.IsAbs(key) || strings.Contains(key, "\\") || strings.Contains(key, "..") {
		return fmt.Errorf("invalid source object key")
	}
	exts := map[string]string{"go": "go", "cpp": "cpp", "python": "py", "java": "java"}
	ext, ok := exts[strings.ToLower(strings.TrimSpace(language))]
	parts := strings.Split(key, "/")
	if !ok || len(parts) != 3 || parts[0] != "sources" || parts[2] != "source."+ext {
		return fmt.Errorf("source object key extension mismatch")
	}
	if _, err := ulid.ParseStrict(parts[1]); err != nil {
		return fmt.Errorf("invalid source object id")
	}
	return nil
}

var _ biz.TaskLoader = (*Loader)(nil)
