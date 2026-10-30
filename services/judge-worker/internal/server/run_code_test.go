package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
)

type runCodeRunner struct {
	compile biz.CompileResult
	run     biz.ExecutionResult
	err     error
	deleted int
	gotCode []byte
	gotIn   []byte
}

func (r *runCodeRunner) Compile(_ context.Context, code []byte) (biz.CompileResult, error) {
	r.gotCode = append([]byte(nil), code...)
	return r.compile, r.err
}
func (r *runCodeRunner) Run(_ context.Context, artifact biz.Artifact, input []byte, limits biz.ResourceLimits) (biz.ExecutionResult, error) {
	if artifact.ID == "" || limits.TimeLimit != runCodeTimeLimit || limits.MemoryBytes != runCodeMemoryMax {
		return biz.ExecutionResult{}, errors.New("unexpected sandbox limits")
	}
	r.gotIn = append([]byte(nil), input...)
	return r.run, r.err
}
func (r *runCodeRunner) DeleteArtifact(_ context.Context, artifact biz.Artifact) error {
	if artifact.ID != "artifact" {
		return errors.New("unexpected artifact")
	}
	r.deleted++
	return nil
}

func TestRunCodeRequiresInternalCredentialAndRejectsUnsafeInputs(t *testing.T) {
	handler := NewRunCodeHandler(map[string]biz.LanguageRunner{"cpp": &runCodeRunner{}}, "01234567890123456789012345678901")
	for _, test := range []struct {
		auth, body string
		status     int
	}{
		{"", `{"language":"cpp","source":"int main(){}","stdin":""}`, http.StatusUnauthorized},
		{"Bearer wrong", `{"language":"cpp","source":"int main(){}","stdin":""}`, http.StatusUnauthorized},
		{"Bearer 01234567890123456789012345678901", `{"language":"bash","source":"echo hi","stdin":""}`, http.StatusBadRequest},
		{"Bearer 01234567890123456789012345678901", `{"language":"cpp","source":"int main(){}","stdin":"","extra":true}`, http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodPost, runCodePath, strings.NewReader(test.body))
		request.Header.Set("Authorization", test.auth)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
		}
	}
}

func TestRunCodeHealthEndpointsDoNotRequireInternalCredential(t *testing.T) {
	handler := NewRunCodeHandler(map[string]biz.LanguageRunner{"cpp": &runCodeRunner{}}, "01234567890123456789012345678901")
	for _, path := range []string{healthPath, readinessPath} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, readinessPath, nil)
	response := httptest.NewRecorder()
	NewRunCodeHandler(nil, "01234567890123456789012345678901").ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty runner readiness status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, readinessPath, nil)
	response = httptest.NewRecorder()
	NewRunCodeHandler(map[string]biz.LanguageRunner{"cpp": &runCodeRunner{}}, "token", func() bool { return false }).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable consumer readiness status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRunCodeCompilesRunsAndDeletesTemporaryArtifact(t *testing.T) {
	runner := &runCodeRunner{
		compile: biz.CompileResult{Artifact: biz.Artifact{ID: "artifact"}, Verdict: biz.VerdictAC},
		run:     biz.ExecutionResult{Verdict: biz.VerdictAC, Output: []byte("42\n"), Time: 12 * time.Millisecond, Memory: 4096},
	}
	handler := NewRunCodeHandler(map[string]biz.LanguageRunner{"cpp": runner}, "01234567890123456789012345678901")
	request := httptest.NewRequest(http.MethodPost, runCodePath, strings.NewReader(`{"language":"cpp","source":"int main(){}","stdin":"in\n"}`))
	request.Header.Set("Authorization", "Bearer 01234567890123456789012345678901")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result runCodeResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Status != "executed" || result.Stdout != "42\n" || result.TimeMS != 12 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if runner.deleted != 1 || string(runner.gotCode) != "int main(){}" || string(runner.gotIn) != "in\n" {
		t.Fatalf("runner state=%+v", runner)
	}
}

func TestRunCodeBoundsOutput(t *testing.T) {
	value := boundRunCodeText(strings.Repeat("x", runCodeOutputMax+100))
	if len(value) != runCodeOutputMax {
		t.Fatalf("length=%d", len(value))
	}
}
