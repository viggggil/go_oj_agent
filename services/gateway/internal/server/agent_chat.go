package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
)

// 普通路由沿用普通预算；只在 Agent Chat 建立独立总预算，保留父级取消。
func requestBudgetFilter(ordinary, agent time.Duration) khttp.FilterFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			budget := ordinary
			if r.Method == http.MethodPost && r.URL.Path == client.AgentChatPath {
				budget = agent
			}
			ctx, cancel := context.WithTimeout(r.Context(), budget)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func registerAgentChatRoute(server *khttp.Server, agent *client.AgentClient) {
	shutdown, stop := context.WithCancel(context.Background())
	server.RegisterOnShutdown(stop)
	server.Route("").POST(client.AgentChatPath, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.AgentChatOperation)
		handler := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			streamCtx, cancel := context.WithCancel(requestCtx)
			defer cancel()
			unsubscribe := context.AfterFunc(shutdown, cancel)
			defer unsubscribe()
			return nil, serveAgentChat(streamCtx, ctx, agent)
		})
		_, err := handler(ctx, nil)
		return err
	})
}

func serveAgentChat(ctx context.Context, httpCtx khttp.Context, agent *client.AgentClient) error {
	if !agent.Enabled() {
		return kerrors.New(503, "GATEWAY_AGENT_DISABLED", "Agent Chat is not enabled")
	}
	if client.ValidateAgentContext(ctx) != nil {
		return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "Invalid Agent request context")
	}
	if !agent.Acquire() {
		return kerrors.New(503, "GATEWAY_AGENT_CAPACITY", "Agent capacity is exhausted")
	}
	defer agent.Release()
	limits := agent.Limits()
	request, w := httpCtx.Request(), httpCtx.Response()
	controller := http.NewResponseController(w)
	// 请求体必须在建流预算内读取，不能被慢速上传无限占用。
	readDeadline := time.Now().Add(limits.HeaderTimeout)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(readDeadline) {
		readDeadline = deadline
	}
	_ = controller.SetReadDeadline(readDeadline)
	request.Body = http.MaxBytesReader(w, request.Body, int64(limits.MaxRequestBytes))
	body, err := io.ReadAll(request.Body)
	_ = controller.SetReadDeadline(time.Time{})
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return kerrors.New(413, "GATEWAY_AGENT_REQUEST_TOO_LARGE", "Agent request exceeds the size limit")
		}
		return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "Cannot read Agent request")
	}
	encoding := request.Header.Get("Content-Encoding")
	if media, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err != nil || media != "application/json" || encoding != "" && encoding != "identity" {
		return kerrors.New(415, "GATEWAY_AGENT_UNSUPPORTED_MEDIA_TYPE", "Agent requires uncompressed JSON")
	}
	if !json.Valid(body) {
		return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "Invalid Agent JSON")
	}
	response, err := agent.Open(ctx, body)
	if err != nil {
		var network net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
			return kerrors.New(504, "GATEWAY_AGENT_TIMEOUT", "Agent did not accept the stream in time")
		}
		return kerrors.New(502, "GATEWAY_AGENT_UNAVAILABLE", "Cannot connect to Agent")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return agentStatusError(response.StatusCode)
	}
	media, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "text/event-stream" || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		return kerrors.New(502, "GATEWAY_AGENT_INVALID_RESPONSE", "Agent did not return SSE")
	}
	identity := agentStreamIdentity{RunID: response.Header.Get("X-Agent-Run-ID"), ConversationID: response.Header.Get("X-Agent-Conversation-ID")}
	if !canonicalUUID(identity.RunID) || !canonicalUUID(identity.ConversationID) {
		return kerrors.New(502, "GATEWAY_AGENT_INVALID_RESPONSE", "Agent stream identity is missing")
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Agent-Run-ID", identity.RunID)
	w.Header().Set("X-Agent-Conversation-ID", identity.ConversationID)
	w.WriteHeader(http.StatusOK)
	if writeAgentFrame(w, nil, limits.WriteTimeout) != nil {
		return nil
	}
	forwardAgentStream(ctx, w, response.Body, &identity, limits)
	return nil
}

func agentStatusError(status int) error {
	code, reason := status, "GATEWAY_AGENT_REJECTED"
	switch status {
	case 400, 404, 409, 413, 415, 503:
	default:
		code, reason = 502, "GATEWAY_AGENT_INVALID_RESPONSE"
	}
	// 不回显上游异常、请求体或凭据。
	return kerrors.New(code, reason, "Agent could not accept the request")
}

type agentToolIdentity struct {
	Tool     string
	Step     int
	Finished bool
}

type agentStreamIdentity struct {
	ToolCalls      map[string]agentToolIdentity `json:"-"`
	RunID          string                       `json:"run_id"`
	ConversationID string                       `json:"conversation_id"`
	Sequence       int                          `json:"sequence"`
}

type agentFrame struct {
	content []byte
	err     error
}

func forwardAgentStream(ctx context.Context, w http.ResponseWriter, body io.ReadCloser, identity *agentStreamIdentity, limits client.AgentLimits) {
	readerCtx, stop := context.WithCancel(ctx)
	frames := make(chan agentFrame, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		reader := bufio.NewReaderSize(body, 4096)
		for {
			content, err := readAgentFrame(reader, limits.MaxFrameBytes)
			select {
			case frames <- agentFrame{content, err}:
			case <-readerCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { stop(); _ = body.Close(); <-finished }()
	idle := time.NewTimer(limits.IdleTimeout)
	defer idle.Stop()
	sendError := func(code string) {
		if ctx.Err() == context.Canceled {
			return
		}
		identity.Sequence++
		payload, _ := json.Marshal(struct {
			Type string `json:"type"`
			*agentStreamIdentity
			Data map[string]string `json:"data"`
		}{"error", identity, map[string]string{"code": code}})
		_ = writeAgentFrame(w, append(append([]byte("event: error\ndata: "), payload...), '\n', '\n'), limits.WriteTimeout)
	}
	for {
		select {
		case <-ctx.Done():
			sendError("AGENT_GATEWAY_TIMEOUT")
			return
		case <-idle.C:
			sendError("AGENT_UPSTREAM_TIMEOUT")
			return
		case frame := <-frames:
			if frame.err != nil {
				sendError("AGENT_UPSTREAM_INTERRUPTED")
				return
			}
			terminal, err := validateAgentFrame(frame.content, identity)
			if err != nil {
				sendError("AGENT_UPSTREAM_INVALID_EVENT")
				return
			}
			if writeAgentFrame(w, frame.content, limits.WriteTimeout) != nil {
				return
			}
			if terminal {
				return
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(limits.IdleTimeout)
		}
	}
}

func readAgentFrame(reader *bufio.Reader, maximum int) ([]byte, error) {
	var frame []byte
	fragmented := false
	for {
		line, err := reader.ReadSlice('\n')
		continued := fragmented
		if len(frame)+len(line) > maximum {
			return nil, errors.New("Agent frame exceeds the size limit")
		}
		frame = append(frame, line...)
		if err == bufio.ErrBufferFull {
			fragmented = true
			continue
		}
		fragmented = false
		if err != nil {
			if err == io.EOF && len(frame) > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if !continued && (bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n"))) {
			return frame, nil
		}
	}
}

var publicAgentErrors = map[string]bool{
	"AGENT_DEADLINE_EXCEEDED": true, "AGENT_RUNTIME_INCOMPLETE": true, "AGENT_EVENT_LIMIT": true,
	"AGENT_RUNTIME_INVALID_EVENT": true, "AGENT_OUTPUT_LIMIT": true, "AGENT_RUNTIME_FAILED": true,
	"AGENT_RUN_FAILED":               true,
	"AGENT_MODEL_AUTH_FAILED":        true,
	"AGENT_MODEL_QUOTA":              true,
	"AGENT_MODEL_RATE_LIMIT":         true,
	"AGENT_MODEL_UNAVAILABLE":        true,
	"AGENT_MODEL_TIMEOUT":            true,
	"AGENT_MODEL_INVALID_RESPONSE":   true,
	"AGENT_MODEL_REJECTED":           true,
	"AGENT_MODEL_OUTPUT_LIMIT":       true,
	"AGENT_MODEL_INPUT_LIMIT":        true,
	"AGENT_MODEL_CALL_LIMIT":         true,
	"AGENT_MODEL_BLOCKED":            true,
	"AGENT_MODEL_UNSUPPORTED_OUTPUT": true,
	"AGENT_MODEL_ENDPOINT_DENIED":    true,
	"AGENT_TOOL_CALL_LIMIT":          true,
	"AGENT_CREDENTIAL_UNAVAILABLE":   true,
}

func validateAgentFrame(frame []byte, identity *agentStreamIdentity) (bool, error) {
	invalid := errors.New("Invalid Agent SSE frame")
	if !utf8.Valid(frame) {
		return false, invalid
	}
	var event string
	var data []string
	for _, line := range strings.Split(string(frame), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return false, invalid
		}
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			if event != "" {
				return false, invalid
			}
			event = value
		case "data":
			data = append(data, value)
		default:
			return false, invalid
		}
	}
	if event == "" && len(data) == 0 {
		return false, nil
	}
	var payload struct {
		Type           string                     `json:"type"`
		RunID          string                     `json:"run_id"`
		ConversationID string                     `json:"conversation_id"`
		Sequence       int                        `json:"sequence"`
		Data           map[string]json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.Join(data, "\n")))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF || payload.Data == nil || payload.Type != event || payload.RunID != identity.RunID || payload.ConversationID != identity.ConversationID || payload.Sequence <= identity.Sequence || payload.Sequence > 10002 {
		return false, invalid
	}
	switch event {
	case "thinking", "token":
		var text string
		raw := payload.Data["text"]
		if len(payload.Data) != 1 || len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &text) != nil || utf8.RuneCountInString(text) > 32000 || event == "thinking" && utf8.RuneCountInString(text) > 256 {
			return false, invalid
		}
	case "progress", "reasoning_summary", "tool_call", "tool_result":
		if !validateAgentActivity(event, payload.Data, identity) {
			return false, invalid
		}
	case "done":
		for _, call := range identity.ToolCalls {
			if !call.Finished {
				return false, invalid
			}
		}
		if len(payload.Data) != 0 {
			return false, invalid
		}
	case "error":
		var code string
		if len(payload.Data) != 1 || json.Unmarshal(payload.Data["code"], &code) != nil || !publicAgentErrors[code] {
			return false, invalid
		}
	default:
		return false, invalid
	}
	identity.Sequence = payload.Sequence
	return event == "done" || event == "error", nil
}

func canonicalUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value
}

func writeAgentFrame(w http.ResponseWriter, frame []byte, timeout time.Duration) error {
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if len(frame) > 0 {
		if _, err := w.Write(frame); err != nil {
			return err
		}
	}
	return controller.Flush()
}

var agentToolName = regexp.MustCompile(`^[a-z][a-z0-9_]{1,127}$`)
var agentCallID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func validateAgentActivity(kind string, data map[string]json.RawMessage, identity *agentStreamIdentity) bool {
	text := func(key string, limit int) (string, bool) {
		var value string
		raw := data[key]
		ok := len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &value) == nil && utf8.RuneCountInString(value) <= limit && value != ""
		return value, ok
	}
	integer := func(key string, min, max int) (int, bool) {
		var value int
		raw := data[key]
		ok := len(raw) > 0 && raw[0] != 'n' && json.Unmarshal(raw, &value) == nil && value >= min && value <= max
		return value, ok
	}
	if kind == "progress" {
		phase, p := text("phase", 32)
		_, m := text("message", 256)
		return len(data) == 2 && p && m && (phase == "preparing" || phase == "analyzing" || phase == "executing_tool" || phase == "answering")
	}
	if kind == "reasoning_summary" {
		_, step := integer("step", 1, 128)
		_, summary := text("text", 400)
		return len(data) == 2 && step && summary
	}
	step, validStep := integer("step", 1, 128)
	callID, validID := text("call_id", 128)
	tool, validTool := text("tool", 128)
	status, validStatus := text("status", 32)
	if !validStep || !validID || !validTool || !validStatus || !agentCallID.MatchString(callID) || !agentToolName.MatchString(tool) {
		return false
	}
	if kind == "tool_call" {
		_, label := text("label", 128)
		if len(data) != 5 || !label || status != "started" {
			return false
		}
		if identity.ToolCalls == nil {
			identity.ToolCalls = make(map[string]agentToolIdentity)
		}
		if _, found := identity.ToolCalls[callID]; found || len(identity.ToolCalls) >= 128 {
			return false
		}
		identity.ToolCalls[callID] = agentToolIdentity{Tool: tool, Step: step}
		return true
	}
	_, duration := integer("duration_ms", 0, 300000)
	_, summary := text("summary", 256)
	call, found := identity.ToolCalls[callID]
	if len(data) != 6 || !duration || !summary || !found || call.Finished || call.Tool != tool || call.Step != step || (status != "succeeded" && status != "failed" && status != "rejected") {
		return false
	}
	call.Finished = true
	identity.ToolCalls[callID] = call
	return true
}
