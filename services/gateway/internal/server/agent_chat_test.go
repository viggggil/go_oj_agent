package server

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

const testRunID = "b056c290-f5a0-49d0-a7b8-949391e53388"
const testConversationID = "363f7d21-b923-41ee-8702-71b2a1280239"

func testAgentFrame(event string, sequence int, data any) string {
	payload, _ := json.Marshal(map[string]any{"type": event, "run_id": testRunID, "conversation_id": testConversationID, "sequence": sequence, "data": data})
	return "event: " + event + "\ndata: " + string(payload) + "\n\n"
}

func setAgentHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Agent-Run-ID", testRunID)
	w.Header().Set("X-Agent-Conversation-ID", testConversationID)
}

func newAgentTestServer(t *testing.T, upstream string, mutate func(*conf.AgentProto), ordinaryTimeout ...string) (*khttp.Server, *internalauth.Verifier) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	private, _ := x509.MarshalPKCS8PrivateKey(key)
	public, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	file := t.TempDir() + "/private.pem"
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.Server.Http.Timeout = "1ns"
	if len(ordinaryTimeout) != 0 {
		config.Server.Http.Timeout = ordinaryTimeout[0]
	}
	config.Auth.InternalPrivateKeyFile = file
	config.Auth.InternalIssuer = "go-oj-gateway"
	config.Auth.InternalKeyId = "gateway-test"
	config.Auth.InternalTokenTtl = "30s"
	settings := &conf.AgentProto{Enabled: true, Endpoint: upstream, MaxDuration: "2s"}
	if mutate != nil {
		mutate(settings)
	}
	config.Clients = &conf.ClientsProto{Agent: settings}
	agent, cleanup, err := client.NewAgentClient(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	auth, err := gatewaymw.NewAuthMiddleware(config)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := internalauth.NewVerifier(map[string][]byte{"gateway-test": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})}, "go-oj-gateway", "agent-service", "gateway-service", time.Minute, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewHTTPServerWithAgent(config, auth, service.NewGatewayService(nil, nil), agent), verifier
}

func agentRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, url+client.AgentChatPath, strings.NewReader(`{"message":"介绍算法"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	r.Header.Set("X-Request-ID", "request-agent")
	r.Header.Set("X-User-ID", "999")
	r.Header.Set("X-Actor-Roles", "root")
	return r
}

func TestAgentProxyStreamsBeforeCompletionAndSignsTrustedIdentity(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	delegations := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-ID") != "" || r.Header.Get("X-Actor-Roles") != "" {
			t.Error("untrusted identity header reached Agent")
		}
		delegations <- strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		setAgentHeaders(w)
		// 首帧跨 HTTP chunk，中文字符也故意跨 chunk。
		first := testAgentFrame("token", 1, map[string]string{"text": "中文"})
		for _, b := range []byte(first) {
			_, _ = w.Write([]byte{b})
			w.(http.Flusher).Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, testAgentFrame("done", 2, map[string]string{}))
	}))
	defer upstream.Close()
	server, verifier := newAgentTestServer(t, upstream.URL, nil)
	gateway := httptest.NewServer(server)
	defer gateway.Close()
	response, err := (&http.Client{Timeout: time.Second}).Do(agentRequest(t, gateway.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	frame, err := readAgentFrame(bufio.NewReader(response.Body), 262144)
	if err != nil || !strings.Contains(string(frame), "中文") {
		t.Fatalf("first frame = %s, %v", frame, err)
	}
	if response.StatusCode != 200 || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("invalid response %#v", response)
	}
	claims, err := verifier.Verify(<-delegations, client.AgentChatOperation)
	if err != nil || claims.ActorID != 1001 || claims.RequestID != "request-agent" || len(claims.ActorRoles) != 2 {
		t.Fatalf("delegation = %#v, %v", claims, err)
	}
	// release 尚未打开，证明 Gateway 没有缓冲到完成。
}

func TestAgentProxyDisconnectCancelsUpstream(t *testing.T) {
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setAgentHeaders(w)
		_, _ = io.WriteString(w, testAgentFrame("thinking", 1, map[string]string{"text": "waiting"}))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	server, _ := newAgentTestServer(t, upstream.URL, nil)
	gateway := httptest.NewServer(server)
	defer gateway.Close()
	ctx, cancel := context.WithCancel(context.Background())
	response, err := http.DefaultClient.Do(agentRequest(t, gateway.URL).WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = response.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream was not cancelled")
	}
}

func TestAgentProxyHTTPFailuresAreRedactedAndNeverRetried(t *testing.T) {
	for _, status := range []int{400, 404, 409, 413, 415, 503, 401, 302, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "private-provider-key")
			}))
			defer upstream.Close()
			server, _ := newAgentTestServer(t, upstream.URL, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, agentRequest(t, "http://gateway"))
			expected := status
			if status == 401 || status == 302 || status == 500 {
				expected = 502
			}
			if recorder.Code != expected || strings.Contains(recorder.Body.String(), "private") || calls.Load() != 1 {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, calls.Load(), recorder.Body.String())
			}
		})
	}
}

func TestAgentProxyRejectsNonSSEAndReportsIncompleteStream(t *testing.T) {
	for _, mode := range []string{"not-sse", "missing-id", "eof", "invalid-event", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "not-sse" {
					_, _ = io.WriteString(w, "private-body")
					return
				}
				setAgentHeaders(w)
				if mode == "missing-id" {
					w.Header().Del("X-Agent-Run-ID")
					return
				}
				if mode == "invalid-event" {
					_, _ = io.WriteString(w, testAgentFrame("error", 1, map[string]string{"code": "PROVIDER_SECRET"}))
					return
				}
				if mode == "oversize" {
					_, _ = io.WriteString(w, strings.Repeat("x", 262145))
					return
				}
				_, _ = io.WriteString(w, testAgentFrame("token", 1, map[string]string{"text": "partial"}))
			}))
			defer upstream.Close()
			server, _ := newAgentTestServer(t, upstream.URL, nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, agentRequest(t, "http://gateway"))
			if mode == "not-sse" || mode == "missing-id" {
				if recorder.Code != 502 {
					t.Fatalf("status=%d", recorder.Code)
				}
			} else if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "event: error") || strings.Contains(recorder.Body.String(), "event: done") {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "private") || strings.Contains(recorder.Body.String(), "PROVIDER_SECRET") {
				t.Fatal("upstream secret exposed")
			}
		})
	}
}

func TestAgentProxyHeaderAndIdleTimeout(t *testing.T) {
	for _, mode := range []string{"headers", "idle"} {
		t.Run(mode, func(t *testing.T) {
			cancelled := make(chan struct{})
			abort := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if mode == "idle" {
					setAgentHeaders(w)
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-abort:
				}
				close(cancelled)
			}))
			defer upstream.Close()
			defer close(abort)
			server, _ := newAgentTestServer(t, upstream.URL, func(c *conf.AgentProto) { c.ResponseHeaderTimeout = "30ms"; c.IdleTimeout = "30ms" })
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, agentRequest(t, "http://gateway"))
			if mode == "headers" && recorder.Code != 504 {
				t.Fatalf("status=%d", recorder.Code)
			}
			if mode == "idle" && (recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "AGENT_UPSTREAM_TIMEOUT")) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("timeout did not cancel upstream")
			}
		})
	}
}

func TestAgentRouteRequiresAuthAndDisabledIsExplicit(t *testing.T) {
	server := newTestHTTPServer(t, &fakeUserClient{})
	for _, authenticated := range []bool{false, true} {
		request := agentRequest(t, "http://gateway")
		want := 503
		if !authenticated {
			request.Header.Del("Authorization")
			want = 401
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("status=%d want=%d", response.Code, want)
		}
	}
}

func TestAgentRequestBudgetPreservesOrdinaryTimeoutAndCancellation(t *testing.T) {
	for _, path := range []string{"/ordinary", client.AgentChatPath} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		parent, cancel := context.WithCancel(request.Context())
		handler := requestBudgetFilter(time.Second, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deadline, ok := r.Context().Deadline()
			remaining := time.Until(deadline)
			if !ok || path == "/ordinary" && remaining > 2*time.Second || path == client.AgentChatPath && remaining < 50*time.Second {
				t.Fatalf("path=%s deadline=%s", path, remaining)
			}
			cancel()
			if r.Context().Err() != context.Canceled {
				t.Fatal("parent cancellation was lost")
			}
		}))
		handler.ServeHTTP(httptest.NewRecorder(), request.WithContext(parent))
		cancel()
	}
}

func TestAgentFrameParserHandlesChunksCRLFAndBoundedLines(t *testing.T) {
	frame := testAgentFrame("token", 1, map[string]string{"text": strings.Repeat("中", 2000)})
	input := ": heartbeat\r\n\r\n" + strings.ReplaceAll(frame, "\n", "\r\n") + testAgentFrame("done", 2, map[string]string{})
	reader := bufio.NewReaderSize(strings.NewReader(input), 16)
	identity := &agentStreamIdentity{RunID: testRunID, ConversationID: testConversationID}
	for index := 0; index < 3; index++ {
		data, err := readAgentFrame(reader, 262144)
		if err != nil {
			t.Fatal(err)
		}
		terminal, err := validateAgentFrame(data, identity)
		if err != nil || terminal != (index == 2) {
			t.Fatalf("index=%d terminal=%t error=%v", index, terminal, err)
		}
	}
	if _, err := readAgentFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", 1025))), 1024); err == nil {
		t.Fatal("unbounded line was accepted")
	}
}

func TestAgentFrameValidationRejectsMalformedPayloads(t *testing.T) {
	valid := testAgentFrame("token", 1, map[string]string{"text": "text"})
	for name, frame := range map[string]string{
		"null data":         testAgentFrame("done", 1, nil),
		"null text":         testAgentFrame("token", 1, map[string]any{"text": nil}),
		"unknown field":     strings.Replace(valid, `"sequence":1`, `"secret":"private","sequence":1`, 1),
		"trailing JSON":     strings.Replace(valid, "\n\n", "{}\n\n", 1),
		"wrong UUID":        strings.ReplaceAll(valid, testRunID, testConversationID),
		"repeated sequence": testAgentFrame("token", 0, map[string]string{"text": "text"}),
		"unknown event":     testAgentFrame("tool_result", 1, map[string]string{"text": "private"}),
		"invalid UTF8":      "event: token\ndata: \xff\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			identity := &agentStreamIdentity{RunID: testRunID, ConversationID: testConversationID}
			if _, err := validateAgentFrame([]byte(frame), identity); err == nil || identity.Sequence != 0 {
				t.Fatalf("malformed frame accepted: %q", frame)
			}
		})
	}
}

func TestAgentProxyTotalTimeoutAndShutdownCancelUpstream(t *testing.T) {
	for _, mode := range []string{"total", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			cancelled, abort := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				setAgentHeaders(w)
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				timer := time.NewTicker(10 * time.Millisecond)
				defer timer.Stop()
				defer close(cancelled)
				for {
					select {
					case <-r.Context().Done():
						return
					case <-abort:
						return
					case <-timer.C:
						_, _ = io.WriteString(w, ": heartbeat\n\n")
						w.(http.Flusher).Flush()
					}
				}
			}))
			defer upstream.Close()
			defer close(abort)
			server, _ := newAgentTestServer(t, upstream.URL, func(c *conf.AgentProto) {
				if mode == "total" {
					c.MaxDuration = "100ms"
				}
			})
			gateway := httptest.NewServer(server)
			defer gateway.Close()
			response, err := (&http.Client{Timeout: time.Second}).Do(agentRequest(t, gateway.URL))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if mode == "shutdown" {
				if err := server.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			body, err := io.ReadAll(response.Body)
			if err != nil || strings.Contains(string(body), "event: done") || mode == "total" && !strings.Contains(string(body), "AGENT_GATEWAY_TIMEOUT") {
				t.Fatalf("body=%s error=%v", body, err)
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("upstream was not cancelled")
			}
		})
	}
}

func TestAgentProxyRejectsRequestLimitsBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	server, _ := newAgentTestServer(t, upstream.URL, func(c *conf.AgentProto) { c.MaxRequestBytes = 1024 })
	for _, mode := range []string{"request ID", "body", "JSON", "encoding", "many roles", "long role", "empty role"} {
		t.Run(mode, func(t *testing.T) {
			request := agentRequest(t, "http://gateway")
			want := 400
			switch mode {
			case "request ID":
				request.Header.Set("X-Request-ID", strings.Repeat("x", 129))
			case "body":
				request.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 1025)))
				want = 413
			case "JSON":
				request.Body = io.NopCloser(strings.NewReader("{"))
			case "encoding":
				request.Header.Set("Content-Encoding", "gzip")
				want = 415
			case "many roles", "long role", "empty role":
				roles := []string{""}
				if mode == "many roles" {
					roles = make([]string, 33)
				} else if mode == "long role" {
					roles = []string{strings.Repeat("r", 65)}
				}
				parts := strings.Split(testAccessToken(t), ".")
				payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
				var claims map[string]any
				if err := json.Unmarshal(payload, &claims); err != nil {
					t.Fatal(err)
				}
				claims["roles"] = roles
				payload, _ = json.Marshal(claims)
				signed := parts[0] + "." + base64.RawURLEncoding.EncodeToString(payload)
				mac := hmac.New(sha256.New, []byte("test-secret"))
				_, _ = mac.Write([]byte(signed))
				request.Header.Set("Authorization", "Bearer "+signed+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != want || calls.Load() != 0 {
				t.Fatalf("status=%d calls=%d", response.Code, calls.Load())
			}
		})
	}
}

type agentWriteFailure struct{ *httptest.ResponseRecorder }

func (w agentWriteFailure) Write([]byte) (int, error) { return 0, errors.New("disconnected") }

type agentTrackedBody struct {
	io.Reader
	closed bool
}

func (b *agentTrackedBody) Close() error { b.closed = true; return nil }

func TestAgentWriteFailureClosesUpstreamReader(t *testing.T) {
	body := &agentTrackedBody{Reader: strings.NewReader(testAgentFrame("token", 1, map[string]string{"text": "partial"}))}
	forwardAgentStream(context.Background(), agentWriteFailure{httptest.NewRecorder()}, body, &agentStreamIdentity{RunID: testRunID, ConversationID: testConversationID}, client.DefaultAgentLimits())
	if !body.closed {
		t.Fatal("upstream reader was not closed")
	}
}
