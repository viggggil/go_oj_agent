package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
)

func TestAgentManagementTrustedDelegationAndErrors(t *testing.T) {
	tokens := make(chan string, 10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens <- strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.Header.Get("X-User-ID") != "" {
			t.Error("forged identity forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			w.WriteHeader(409)
			io.WriteString(w, `{"code":"AGENT_CONFIGURATION_STALE"}`)
			return
		}
		io.WriteString(w, `{"configuration":{"id":"id","kind":"prompt","key":"test_key","archived":false,"disabled":false,"created_at":"now","created_by":7,"archived_at":null,"archived_by":null,"content":{"text":"test","variables":[]}}}`)
	}))
	defer upstream.Close()
	server, verifier := newAgentTestServer(t, upstream.URL, nil, "2s")
	// 普通管理查询沿用普通预算，测试配置中的 1ns 只用于既有 SSE 测试。
	gateway := httptest.NewServer(server)
	defer gateway.Close()
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		path := "/api/v1/admin/agent/prompts/test_key"
		r, _ := http.NewRequest(method, gateway.URL+path, strings.NewReader(`{"expected_id":"`+uuid.NewString()+`","content":{"text":"test"}}`))
		if method == http.MethodGet {
			r.Body = http.NoBody
		}
		r.Header.Set("Authorization", "Bearer "+testAccessToken(t))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Request-ID", uuid.NewString())
		r.Header.Set("X-User-ID", "999")
		response, err := (&http.Client{Timeout: time.Second}).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		want := 200
		if method == http.MethodPut {
			want = 409
		}
		if response.StatusCode != want {
			t.Fatalf("%s: %d %s", method, response.StatusCode, data)
		}
		claims, err := verifier.Verify(<-tokens, "HTTP "+method+" "+path)
		if err != nil || claims.ActorID != 1001 || claims.RequestID != r.Header.Get("X-Request-ID") {
			t.Fatalf("claims %#v %v", claims, err)
		}
		if method == http.MethodPut {
			var body map[string]any
			json.Unmarshal(data, &body)
			if body["reason"] != "AGENT_CONFIGURATION_STALE" {
				t.Fatal(string(data))
			}
		}
	}
}

func TestAgentManagementContractRejectsUnexpectedData(t *testing.T) {
	for _, test := range []struct{ path, data string }{
		{"/api/v1/agent/agents", `{"configuration":{"content":{"text":"private prompt"}}}`},
		{"/api/v1/agent/agents", `{"items":[{"key":"test_key","name":"test","is_default":false,"prompt_text":"private"}],"page":{"page":1,"page_size":20,"total":1}}`},
		{"/api/v1/agent/agents", `{"items":[{"key":"test_key","name":{"text":"private"},"is_default":false}],"page":{"page":1,"page_size":20,"total":1}}`},
		{"/api/v1/admin/agent/model-options", `{"items":[{"id":"id","key":"model","model":"flash","temperature":null,"verbosity":null,"max_output_tokens":10,"context_window_tokens":20,"credential_id":"private"}],"page":{"page":1,"page_size":20,"total":1}}`},
		{"/api/v1/admin/agent/prompts", `{"items":[],"page":{"page":0,"page_size":20,"total":0}}`},
		{"/api/v1/admin/agent/prompts", `{"items":[],"page":{"page":1,"page_size":20,"total":0},"other":"private"}`},
	} {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(test.data), &payload); err != nil {
			t.Fatal(err)
		}
		if validAgentJSONEnvelope("GET", test.path, payload) {
			t.Fatalf("accepted %s", test.data)
		}
	}
}

func TestAgentManagementLimitsPermissionsAndTimeout(t *testing.T) {
	for _, scenario := range []string{"oversized response", "wrong type", "deadline", "user", "missing auth", "missing request ID", "oversized request"} {
		t.Run(scenario, func(t *testing.T) {
			calls := make(chan struct{}, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- struct{}{}
				w.Header().Set("Content-Type", "application/json")
				switch scenario {
				case "oversized response":
					io.WriteString(w, strings.Repeat("x", maxAgentJSONBytes+1))
				case "wrong type":
					io.WriteString(w, `{"items":[{"key":"test","name":{"secret":"private"},"is_default":false}],"page":{"page":1,"page_size":20,"total":1}}`)
				case "deadline":
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				default:
					io.WriteString(w, `{"items":[],"page":{"page":1,"page_size":20,"total":0}}`)
				}
			}))
			defer upstream.Close()
			budget := "2s"
			if scenario == "deadline" {
				budget = "100ms"
			}
			server, _ := newAgentTestServer(t, upstream.URL, nil, budget)
			gateway := httptest.NewServer(server)
			defer gateway.Close()
			path, method, body := "/api/v1/admin/agent/prompts", http.MethodGet, ""
			want := 502
			if scenario == "wrong type" {
				path = "/api/v1/agent/agents"
			}
			if scenario == "deadline" {
				want = 504
			}
			if scenario == "user" {
				want = 403
			}
			if scenario == "missing auth" {
				want = 401
			}
			if scenario == "missing request ID" {
				method, body, want = http.MethodPost, `{}`, 400
			}
			if scenario == "oversized request" {
				method, body, want = http.MethodPost, strings.Repeat("x", 262145), 413
			}
			r, _ := http.NewRequest(method, gateway.URL+path, strings.NewReader(body))
			token := testAccessToken(t)
			if scenario == "user" {
				parts := strings.Split(token, ".")
				decoded, _ := base64.RawURLEncoding.DecodeString(parts[1])
				var claims map[string]any
				json.Unmarshal(decoded, &claims)
				claims["roles"] = []string{"user"}
				decoded, _ = json.Marshal(claims)
				unsigned := parts[0] + "." + base64.RawURLEncoding.EncodeToString(decoded)
				signer := hmac.New(sha256.New, []byte(testConfig().Auth.GetAccessTokenKey()))
				signer.Write([]byte(unsigned))
				token = unsigned + "." + base64.RawURLEncoding.EncodeToString(signer.Sum(nil))
			}
			if scenario != "missing auth" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			if scenario != "missing request ID" {
				r.Header.Set("X-Request-ID", uuid.NewString())
			}
			r.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 3 * time.Second}).Do(r)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != want {
				t.Fatalf("%d want %d: %s", response.StatusCode, want, data)
			}
			if strings.Contains(string(data), "private") {
				t.Fatal("sensitive upstream response leaked")
			}
			if want == 400 || want == 401 || want == 403 || want == 413 {
				select {
				case <-calls:
					t.Fatal("rejected request reached upstream")
				default:
				}
			}
		})
	}
}

func TestAgentOperationRejectsUnknownPathsAndRoles(t *testing.T) {
	for _, path := range []string{"/api/v1/admin/agent/providers", "/api/v1/admin/agent/prompts/x/archive", "/api/v1/admin/agent/prompts/test_key/versions/not-a-uuid", "/api/v1/admin/agent/prompts/test-key"} {
		if _, err := client.AgentOperation(http.MethodGet, path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	if client.AgentAdministrator([]string{"user", "root"}) {
		t.Fatal("untrusted role accepted")
	}
	for _, role := range []string{"admin", "system_admin", "agent_admin"} {
		if !client.AgentAdministrator([]string{role}) {
			t.Fatal(role)
		}
	}
	id := uuid.NewString()
	if _, err := client.AgentOperation(http.MethodGet, "/api/v1/admin/agent/evals/datasets"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentOperation(http.MethodPost, "/api/v1/admin/agent/evals/datasets"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentOperation(http.MethodGet, "/api/v1/admin/agent/evals/datasets/"+id); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentOperation(http.MethodPost, "/api/v1/admin/agent/evals/runs"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentOperation(http.MethodGet, "/api/v1/admin/agent/evals/runs/"+id+"/results"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentOperation(http.MethodPost, "/api/v1/admin/agent/evals/runs/"+id+"/cancel"); err != nil {
		t.Fatal(err)
	}
}

func TestEvalDatasetEnvelopeContract(t *testing.T) {
	id := uuid.NewString()
	dataset := map[string]any{"id": id, "name": "core", "description": "", "format_version": "v1", "content_hash": strings.Repeat("a", 64), "case_count": 1, "created_by": 7, "created_at": "now", "status": "READY"}
	value := map[string]any{"dataset": dataset, "duplicate": false}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil || !validEvalEnvelope(http.MethodPost, "/api/v1/admin/agent/evals/datasets", payload) {
		t.Fatal("valid eval dataset rejected")
	}
	value["private"] = "must reject"
	data, _ = json.Marshal(value)
	_ = json.Unmarshal(data, &payload)
	if validEvalEnvelope(http.MethodPost, "/api/v1/admin/agent/evals/datasets", payload) {
		t.Fatal("private eval field accepted")
	}
}

func TestEvalDatasetErrorEnvelopeIncludesBoundedLine(t *testing.T) {
	data := map[string]json.RawMessage{
		"code":    json.RawMessage(`"AGENT_EVAL_DATASET_INVALID"`),
		"message": json.RawMessage(`"line 2 is invalid"`),
		"line":    json.RawMessage(`2`),
	}
	if !validEvalError(data) {
		t.Fatal("valid eval error rejected")
	}
	data["private"] = json.RawMessage(`"reject"`)
	if validEvalError(data) {
		t.Fatal("private eval error field accepted")
	}
}

func TestRAGEvidenceRouteContractRejectsUnexpectedPrivateFields(t *testing.T) {
	id := uuid.NewString()
	path := "/api/v1/admin/agent/rag/runs/" + id + "/evidence"
	if _, err := client.AgentOperation(http.MethodGet, path); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentOperation(http.MethodPost, path); err == nil {
		t.Fatal("evidence write accepted")
	}
	source := map[string]any{"source_id": id, "base_id": id, "document_id": id, "version_id": id, "problem_id": 22, "title": "单调队列", "number": 0, "section": "证明", "start": 0, "end": 10, "score": 0.8}
	envelope := map[string]any{"evidence": map[string]any{"run_id": id, "status": "COMPLETED", "agent_key": "rag_agent", "scope": []string{id}, "versions": []string{id}, "attempts": []any{map[string]any{"call_id": "call_1", "ok": true, "error_code": nil, "sources": []any{source}}}}}
	encode := func() map[string]json.RawMessage {
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	if !validRAGEnvelope(path, encode()) {
		t.Fatal("valid evidence rejected")
	}
	for _, field := range []string{"text", "object_key", "content", "provider_config"} {
		source[field] = "private-content"
		if validRAGEnvelope(path, encode()) {
			t.Fatalf("private field %s accepted", field)
		}
		delete(source, field)
	}
}

func TestAgentHistoricalLookupRoutesAndRedaction(t *testing.T) {
	identifier := uuid.NewString()
	for _, path := range []string{"/api/v1/admin/agent/model-options/" + identifier, "/api/v1/admin/agent/prompts/versions/" + identifier} {
		if _, err := client.AgentOperation(http.MethodGet, path); err != nil {
			t.Fatal(err)
		}
		if _, err := client.AgentOperation(http.MethodPut, path); err == nil {
			t.Fatal("historical lookup accepted write")
		}
	}
	model := `{"model_option":{"id":"` + identifier + `","key":"su8_flash","model":"deepseek-v4-flash","temperature":null,"verbosity":null,"max_output_tokens":8000,"context_window_tokens":32000,"archived":true,"disabled":false,"available":false}}`
	prompt := `{"configuration":{"id":"` + identifier + `","kind":"prompt","key":"test_prompt","archived":true,"disabled":false,"created_at":"now","created_by":7,"archived_at":"now","archived_by":7,"content":{"text":"historical","variables":[]}}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/versions/versions") {
			io.WriteString(w, `{"items":[],"page":{"page":1,"page_size":20,"total":0}}`)
		} else if strings.Contains(r.URL.Path, "model-options") {
			io.WriteString(w, model)
		} else {
			io.WriteString(w, prompt)
		}
	}))
	defer upstream.Close()
	server, _ := newAgentTestServer(t, upstream.URL, nil, "2s")
	gateway := httptest.NewServer(server)
	defer gateway.Close()
	for _, path := range []string{"/api/v1/admin/agent/model-options/" + identifier, "/api/v1/admin/agent/prompts/versions/" + identifier} {
		r, _ := http.NewRequest(http.MethodGet, gateway.URL+path, nil)
		r.Header.Set("Authorization", "Bearer "+testAccessToken(t))
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, response.StatusCode, body)
		}
	}
	for _, resource := range []string{"agents", "prompts", "skills"} {
		path := "/api/v1/admin/agent/" + resource + "/versions/versions"
		r, _ := http.NewRequest(http.MethodGet, gateway.URL+path, nil)
		r.Header.Set("Authorization", "Bearer "+testAccessToken(t))
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || !strings.Contains(string(body), `"items"`) {
			t.Fatalf("legacy versions key %s: %d %s", path, response.StatusCode, body)
		}
	}
	var payload map[string]json.RawMessage
	json.Unmarshal([]byte(model), &payload)
	path := "/api/v1/admin/agent/model-options/" + identifier
	if !validAgentJSONEnvelope("GET", path, payload) {
		t.Fatal("redacted model rejected")
	}
	var value map[string]json.RawMessage
	json.Unmarshal(payload["model_option"], &value)
	value["credential_id"] = json.RawMessage(`"private"`)
	payload["model_option"], _ = json.Marshal(value)
	if validAgentJSONEnvelope("GET", path, payload) {
		t.Fatal("credential data accepted")
	}
}

func TestEvalResultListAndDetailHaveSeparateBoundedContracts(t *testing.T) {
	common := `"id":"id","run_id":"run","case_id":"case","attempt":1,"status":"COMPLETED","checks":{"pass":true,"hard_fail":false},"created_at":"2026-10-16T00:00:00Z","error_code":null,"latency_ms":10,"finished_at":null,"review_version":0,"manual_review":null,"effective_pass":true,"model_review_status":"NOT_REQUESTED","model_review_attempt":0,"model_review":null,"model_review_error_code":null,"category":"basic","title":"例子"`
	preview := json.RawMessage(`{` + common + `,"answer_preview":"预览"}`)
	detail := json.RawMessage(`{` + common + `,"answer":"完整回答","model_summary":null,"case":{}}`)
	if !validEvalResult(preview, false) || !validEvalResult(detail, true) {
		t.Fatal("valid list/detail response rejected")
	}
	if validEvalResult(detail, false) || validEvalResult(preview, true) {
		t.Fatal("list/detail response contracts must not be interchanged")
	}
}

func TestAgentHistorySanitizedToolTraceContract(t *testing.T) {
	path := "/api/v1/agent/conversations/" + testConversationID + "/messages"
	trace := `{"step":1,"call_id":"call_1","tool_name":"get_problem","status":"succeeded","duration_ms":4,"summary":"已获取授权信息"}`
	makePayload := func(steps string) map[string]json.RawMessage {
		var payload map[string]json.RawMessage
		_ = json.Unmarshal([]byte(`{"conversation":{"id":"`+testConversationID+`","agent_key":"assistant","title":"历史"},"items":[{"id":1,"role":"user","content":"问题","status":"FAILED","error_code":"AGENT_MODEL_TIMEOUT","tool_steps":[`+steps+`]}],"next_before":null}`), &payload)
		return payload
	}
	if !validAgentJSONEnvelope("GET", path, makePayload(trace)) {
		t.Fatal("sanitized failed-run trace rejected")
	}
	for _, bad := range []string{strings.Replace(trace, `"step":1`, `"step":true`, 1), strings.Replace(trace, `"summary":`, `"output":"private","summary":`, 1), strings.Replace(trace, `"status":"succeeded"`, `"status":"invented"`, 1)} {
		if validAgentJSONEnvelope("GET", path, makePayload(bad)) {
			t.Fatal("unsafe trace accepted")
		}
	}
	for _, content := range []string{`{"key":"assistant","name":"助手","default":true}`, `{"key":"assistant","name":"助手","default":true,"execution_mode":"direct"}`} {
		var payload map[string]json.RawMessage
		_ = json.Unmarshal([]byte(`{"items":[`+content+`],"default_skill_key":"assistant"}`), &payload)
		if !validAgentJSONEnvelope("GET", "/api/v1/agent/agents/assistant/skills", payload) {
			t.Fatal("old/new skill directory rejected")
		}
	}
}
