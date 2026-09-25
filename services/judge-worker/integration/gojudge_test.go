package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	judgecontract "github.com/viggggil/go_oj_agent/pkg/judge"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/comparator"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/language"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

type staticLoader struct{ loaded biz.LoadedTask }

func (s staticLoader) Load(context.Context, mq.JudgeTask) (biz.LoadedTask, error) {
	return s.loaded, nil
}

func TestGoJudgeEngine(t *testing.T) {
	endpoint := os.Getenv("GO_JUDGE_TEST_ENDPOINT")
	token := os.Getenv("GO_JUDGE_TEST_TOKEN")
	if endpoint == "" || token == "" {
		t.Skip("set GO_JUDGE_TEST_ENDPOINT and GO_JUDGE_TEST_TOKEN")
	}
	adapter, cleanup, err := sandbox.NewGoJudge(endpoint, token, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	runner := language.NewGoRunner(adapter, language.GoConfig{
		CompileTimeLimit: 5 * time.Second, CompileMemoryBytes: 256 << 20,
		ProcessLimit: 16, OutputLimitBytes: 64 << 10,
	})

	tests := []struct {
		name, source, expected, verdict string
		timeLimitMS, memoryLimitKB      int32
	}{
		{"accepted", `package main
import ("bufio"; "fmt"; "os")
func main(){ in:=bufio.NewReader(os.Stdin); var a,b int; fmt.Fscan(in,&a,&b); fmt.Println(a+b) }
`, "3\n", biz.VerdictAC, 1000, 65536},
		{"wrong-answer", `package main
import "fmt"
func main(){ fmt.Println(4) }
`, "3\n", biz.VerdictWA, 1000, 65536},
		{"compile-error", `package main
func main() { this is invalid }
`, "", biz.VerdictCE, 1000, 65536},
		{"time-limit", `package main
func main(){ for {} }
`, "", biz.VerdictTLE, 100, 65536},
		{"memory-limit", `package main
func main(){ b:=make([]byte, 128<<20); b[0]=1; select{} }
`, "", biz.VerdictMLE, 1000, 16384},
		{"runtime-error", `package main
func main(){ panic("boom") }
`, "", biz.VerdictRE, 1000, 65536},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task, loaded := integrationTask(int64(index+1), test.source, test.expected, test.timeLimitMS, test.memoryLimitKB)
			engine := biz.NewEngine(staticLoader{loaded: loaded}, runner, comparator.NewText())
			outcome := engine.Execute(t.Context(), task)
			if outcome.Failed != nil || outcome.Completed == nil || outcome.Completed.Verdict != test.verdict {
				t.Fatalf("outcome = %+v", outcome)
			}
		})
	}
}

func integrationTask(submissionID int64, source, expected string, timeLimitMS, memoryLimitKB int32) (mq.JudgeTask, biz.LoadedTask) {
	revision := "01K5C6Y7N8P9Q0R1S2T3V4W5X6"
	task := mq.JudgeTask{
		SubmissionID: submissionID, ProblemID: 7, Language: "go", JudgeRevision: revision,
		SourceObjectKey: "sources/integration/source.go", SourceSHA256: strings.Repeat("a", 64),
		SourceSizeBytes: int64(len(source)), JudgeDeadlineAt: time.Now().Add(time.Minute),
	}
	manifest := judgecontract.Manifest{
		ManifestVersion: judgecontract.ManifestVersion, ProblemID: 7, JudgeRevision: revision,
		TimeLimitMS: timeLimitMS, MemoryLimitKB: memoryLimitKB,
		Testcases: []judgecontract.Testcase{{
			CaseNo: 1,
			Input:  judgecontract.Object{ObjectKey: "problem-7/judge-revisions/" + revision + "/testcases/1.in", SHA256: strings.Repeat("b", 64), SizeBytes: 4},
			Output: judgecontract.Object{ObjectKey: "problem-7/judge-revisions/" + revision + "/testcases/1.out", SHA256: strings.Repeat("c", 64), SizeBytes: int64(len(expected)) + 1},
		}},
	}
	return task, biz.LoadedTask{
		Task: task, Source: []byte(source), Manifest: manifest,
		Cases: []biz.LoadedCase{{CaseNo: 1, Input: []byte("1 2\n"), Expected: []byte(expected)}},
	}
}
