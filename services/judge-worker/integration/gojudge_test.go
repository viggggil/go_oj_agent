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
		CompileTimeLimit: 60 * time.Second, CompileMemoryBytes: 512 << 20,
		CompileOutputBytes: 64 << 20,
		ProcessLimit:       16, OutputLimitBytes: 64 << 10,
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
			verdictOK := outcome.Completed != nil && outcome.Completed.Verdict == test.verdict
			// Go's runtime may terminate with a non-zero exit before the sandbox
			// cgroup reports MLE when the limit is below its startup footprint.
			if test.name == "memory-limit" && outcome.Completed != nil {
				verdictOK = outcome.Completed.Verdict == biz.VerdictMLE || outcome.Completed.Verdict == biz.VerdictRE
			}
			if outcome.Failed != nil || !verdictOK {
				t.Fatalf("outcome = %+v failed=%+v completed=%+v", outcome, outcome.Failed, outcome.Completed)
			}
		})
	}
}

func TestCommandLanguageEngines(t *testing.T) {
	endpoint := os.Getenv("GO_JUDGE_TEST_ENDPOINT")
	token := os.Getenv("GO_JUDGE_TEST_TOKEN")
	if endpoint == "" || token == "" {
		t.Skip("set GO_JUDGE_TEST_ENDPOINT and GO_JUDGE_TEST_TOKEN")
	}
	adapter, cleanup, err := sandbox.NewGoJudge(endpoint, token, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	tests := []struct {
		language, source, extension string
	}{
		{"c", `#include <stdio.h>
int main(void) { int a, b; if (scanf("%d %d", &a, &b) != 2) return 1; printf("%d\n", a + b); return 0; }
`, "c"},
		{"cpp", `#include <iostream>
int main() { int a, b; if (!(std::cin >> a >> b)) return 1; std::cout << a + b << "\n"; }
`, "cpp"},
		{"java", `import java.util.*;
public class Main { public static void main(String[] args) { Scanner s = new Scanner(System.in); System.out.println(s.nextInt() + s.nextInt()); } }
`, "java"},
		{"python", `a, b = map(int, input().split())
print(a + b)
`, "py"},
	}
	for index, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			var runner biz.LanguageRunner
			switch test.language {
			case "c":
				runner = language.NewCommandRunner(adapter, "c", "/usr/bin/gcc", "main.c", "main", func(source string) []string { return []string{"-std=c17", "-O2", "-pipe", "-o", "main", source} })
			case "cpp":
				runner = language.NewCommandRunner(adapter, "cpp", "/usr/bin/g++", "main.cpp", "main", func(source string) []string { return []string{"-std=c++17", "-O2", "-pipe", "-o", "main", source} })
			case "java":
				runner = language.NewCommandRunner(adapter, "java", "/usr/bin/javac", "Main.java", "Main.class", func(source string) []string { return []string{"-encoding", "UTF-8", source} })
			case "python":
				runner = language.NewCommandRunner(adapter, "python", "/usr/bin/python3", "main.py", "main.py", func(source string) []string { return []string{"-m", "py_compile", source} })
			}
			task, loaded := integrationTaskForLanguage(int64(100+index), test.language, test.extension, test.source, "3\n", 2000, 65536)
			engine := biz.NewEngineWithRunners(staticLoader{loaded: loaded}, map[string]biz.LanguageRunner{test.language: runner}, comparator.NewText())
			outcome := engine.Execute(t.Context(), task)
			if outcome.Failed != nil || outcome.Completed == nil || outcome.Completed.Verdict != biz.VerdictAC {
				t.Fatalf("outcome = %+v failed=%+v completed=%+v", outcome, outcome.Failed, outcome.Completed)
			}
		})
	}
}

func integrationTask(submissionID int64, source, expected string, timeLimitMS, memoryLimitKB int32) (mq.JudgeTask, biz.LoadedTask) {
	return integrationTaskForLanguage(submissionID, "go", "go", source, expected, timeLimitMS, memoryLimitKB)
}

func integrationTaskForLanguage(submissionID int64, language, extension, source, expected string, timeLimitMS, memoryLimitKB int32) (mq.JudgeTask, biz.LoadedTask) {
	revision := "01K5C6Y7N8P9Q0R1S2T3V4W5X6"
	task := mq.JudgeTask{
		SubmissionID: submissionID, ProblemID: 7, Language: language, JudgeRevision: revision,
		SourceObjectKey: "sources/integration/source." + extension, SourceSHA256: strings.Repeat("a", 64),
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
