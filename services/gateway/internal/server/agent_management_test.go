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
}
