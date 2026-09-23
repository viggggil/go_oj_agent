package judge

import (
	"encoding/hex"
	"fmt"
)

const ManifestVersion int32 = 1

type Manifest struct {
	ManifestVersion int32      `json:"manifest_version"`
	ProblemID       int64      `json:"problem_id"`
	JudgeRevision   string     `json:"judge_revision"`
	TimeLimitMS     int32      `json:"time_limit_ms"`
	MemoryLimitKB   int32      `json:"memory_limit_kb"`
	Testcases       []Testcase `json:"testcases"`
}

type Testcase struct {
	CaseNo int32  `json:"case_no"`
	Input  Object `json:"input"`
	Output Object `json:"output"`
}

type Object struct {
	ObjectKey string `json:"object_key"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

func ManifestObjectKey(problemID int64, judgeRevision string) (string, error) {
	if problemID <= 0 || len(judgeRevision) != 26 {
		return "", fmt.Errorf("invalid judge revision identity")
	}
	return fmt.Sprintf("problem-%d/judge-revisions/%s/manifest.json", problemID, judgeRevision), nil
}

func (m Manifest) Validate() error {
	if m.ManifestVersion != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %d", m.ManifestVersion)
	}
	if m.ProblemID <= 0 || len(m.JudgeRevision) != 26 {
		return fmt.Errorf("invalid manifest identity")
	}
	if m.TimeLimitMS <= 0 || m.MemoryLimitKB <= 0 {
		return fmt.Errorf("invalid manifest resource limits")
	}
	if len(m.Testcases) == 0 {
		return fmt.Errorf("manifest has no testcases")
	}
	seen := make(map[int32]struct{}, len(m.Testcases))
	for _, testcase := range m.Testcases {
		if testcase.CaseNo <= 0 {
			return fmt.Errorf("invalid testcase number")
		}
		if _, ok := seen[testcase.CaseNo]; ok {
			return fmt.Errorf("duplicate testcase number %d", testcase.CaseNo)
		}
		seen[testcase.CaseNo] = struct{}{}
		if err := testcase.Input.validate(); err != nil {
			return fmt.Errorf("testcase %d input: %w", testcase.CaseNo, err)
		}
		if err := testcase.Output.validate(); err != nil {
			return fmt.Errorf("testcase %d output: %w", testcase.CaseNo, err)
		}
	}
	return nil
}

func (o Object) validate() error {
	if o.ObjectKey == "" || o.SizeBytes <= 0 {
		return fmt.Errorf("invalid object metadata")
	}
	digest, err := hex.DecodeString(o.SHA256)
	if err != nil || len(digest) != 32 {
		return fmt.Errorf("invalid sha256")
	}
	return nil
}
