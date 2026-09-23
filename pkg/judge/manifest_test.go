package judge

import (
	"strings"
	"testing"
)

func TestManifestValidate(t *testing.T) {
	manifest := Manifest{
		ManifestVersion: ManifestVersion,
		ProblemID:       7,
		JudgeRevision:   "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
		TimeLimitMS:     1000,
		MemoryLimitKB:   65536,
		Testcases: []Testcase{{
			CaseNo: 1,
			Input:  Object{ObjectKey: "problem-7/revision/1.in", SHA256: strings.Repeat("a", 64), SizeBytes: 4},
			Output: Object{ObjectKey: "problem-7/revision/1.out", SHA256: strings.Repeat("b", 64), SizeBytes: 2},
		}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	manifest.TimeLimitMS = 0
	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate() expected resource limit error")
	}
}

func TestManifestObjectKey(t *testing.T) {
	key, err := ManifestObjectKey(7, "01K5C6Y7N8P9Q0R1S2T3V4W5X6")
	if err != nil || key != "problem-7/judge-revisions/01K5C6Y7N8P9Q0R1S2T3V4W5X6/manifest.json" {
		t.Fatalf("ManifestObjectKey() = %q, %v", key, err)
	}
}
