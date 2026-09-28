package language

import (
	"strings"
	"testing"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

func TestCommandRunnersBuildLanguageSpecificRequests(t *testing.T) {
	tests := []struct{ language, compiler, source, artifact string }{
		{"c", "/usr/bin/gcc", "main.c", "main"},
		{"cpp", "/usr/bin/g++", "main.cpp", "main"},
		{"java", "/usr/bin/javac", "Main.java", "Main.class"},
		{"python", "/usr/bin/python3", "main.py", "main.py"},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			executor := &fakeSandbox{results: []sandbox.Result{{Status: sandbox.StatusAccepted, FileIDs: map[string]string{test.artifact: "artifact"}}}}
			runner := NewCommandRunner(executor, test.language, test.compiler, test.source, test.artifact, func(source string) []string { return []string{source} })
			result, err := runner.Compile(t.Context(), []byte("source"))
			if err != nil || result.Verdict != biz.VerdictAC || result.Artifact.ID != "artifact" {
				t.Fatalf("Compile() = %+v, %v", result, err)
			}
			request := executor.requests[0]
			if request.Args[0] != test.compiler || request.CopyIn[test.source].Content == nil || request.CacheOut[0] != test.artifact {
				t.Fatalf("compile request = %+v", request)
			}
			if !strings.Contains(strings.Join(request.Env, "\n"), "LANG=C.UTF-8") {
				t.Fatalf("compile environment = %v", request.Env)
			}
		})
	}
}

func TestCommandRunnerCompileSandboxFailureIsRetryable(t *testing.T) {
	runner := NewCommandRunner(&fakeSandbox{results: []sandbox.Result{{Status: sandbox.StatusTimeLimitExceeded}}}, "cpp", "/usr/bin/g++", "main.cpp", "main", nil)
	_, err := runner.Compile(t.Context(), []byte("int main(){}"))
	reason, retryable := biz.ClassifySystemError(err)
	if reason != "SANDBOX_COMPILE_TIMEOUT" || !retryable {
		t.Fatalf("error = %q retryable=%v", reason, retryable)
	}
}
