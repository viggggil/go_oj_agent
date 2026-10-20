package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

const maxAgentJSONBytes = 1 << 20

func registerAgentManagementRoutes(server *khttp.Server, agent *client.AgentClient) {
	routes := []struct {
		path    string
		methods []string
	}{
		{"/api/v1/admin/agent/rag/knowledge-bases", []string{http.MethodGet, http.MethodPost}},
		{"/api/v1/admin/agent/rag/knowledge-bases/{base_id:[0-9a-f-]{36}}", []string{http.MethodGet}},
		{"/api/v1/admin/agent/rag/knowledge-bases/{base_id:[0-9a-f-]{36}}/rename", []string{http.MethodPost}},
		{"/api/v1/admin/agent/rag/documents", []string{http.MethodGet, http.MethodPost}},
		{"/api/v1/admin/agent/rag/documents/{document_id:[0-9a-f-]{36}}", []string{http.MethodGet}},
		{"/api/v1/admin/agent/rag/documents/{document_id:[0-9a-f-]{36}}/versions", []string{http.MethodGet}},
		{"/api/v1/admin/agent/rag/documents/{document_id:[0-9a-f-]{36}}/{action:enable|archive|retry}", []string{http.MethodPost}},
		{"/api/v1/admin/agent/rag/search", []string{http.MethodPost}},
		{"/api/v1/admin/agent/rag/preview", []string{http.MethodPost}},
		{"/api/v1/admin/agent/rag/runs/{run_id:[0-9a-f-]{36}}/evidence", []string{http.MethodGet}},
		{"/api/v1/admin/agent/evals/datasets", []string{http.MethodGet, http.MethodPost}},
		{"/api/v1/admin/agent/evals/datasets/{dataset_id:[0-9a-f-]{36}}", []string{http.MethodGet}},
		{"/api/v1/admin/agent/evals/datasets/{dataset_id:[0-9a-f-]{36}}/cases", []string{http.MethodGet}},
		{"/api/v1/admin/agent/evals/results/{result_id:[0-9a-f-]{36}}/review", []string{http.MethodPost}},
		{"/api/v1/admin/agent/evals/results/{result_id:[0-9a-f-]{36}}/reviews", []string{http.MethodGet}},
		{"/api/v1/admin/agent/evals/results/{result_id:[0-9a-f-]{36}}/review-agent", []string{http.MethodPost}},
		{"/api/v1/admin/agent/evals/results/{result_id:[0-9a-f-]{36}}/model-reviews", []string{http.MethodGet}},
		{"/api/v1/admin/agent/evals/runs", []string{http.MethodGet, http.MethodPost}},
		{"/api/v1/admin/agent/evals/runs/{run_id:[0-9a-f-]{36}}", []string{http.MethodGet}},
		{"/api/v1/admin/agent/evals/runs/{run_id:[0-9a-f-]{36}}/cancel", []string{http.MethodPost}},
		{"/api/v1/admin/agent/evals/runs/{run_id:[0-9a-f-]{36}}/results", []string{http.MethodGet}},
		{"/api/v1/admin/agent/evals/runs/{run_id:[0-9a-f-]{36}}/results/{result_id:[0-9a-f-]{36}}", []string{http.MethodGet}},
		{"/api/v1/agent/agents", []string{http.MethodGet}},
		{"/api/v1/agent/conversations", []string{http.MethodGet}},
		{"/api/v1/agent/conversations/{conversation_id:[0-9a-f-]{36}}/messages", []string{http.MethodGet}},
		{"/api/v1/agent/agents/{agent_key}/skills", []string{http.MethodGet}},
		{"/api/v1/admin/agent/model-options", []string{http.MethodGet}},
		{"/api/v1/admin/agent/model-options/{identifier}", []string{http.MethodGet}},
		{"/api/v1/admin/agent/tools", []string{http.MethodGet}},
		{"/api/v1/admin/agent/tools/{tool_name:[a-z][a-z0-9_]{1,127}}", []string{http.MethodPut}},
	}
	for _, resource := range []string{"agents", "prompts", "skills"} {
		path := "/api/v1/admin/agent/" + resource
		routes = append(routes,
			struct {
				path    string
				methods []string
			}{path + "/versions/{identifier:[0-9a-f-]{36}}", []string{http.MethodGet}},
			struct {
				path    string
				methods []string
			}{path, []string{http.MethodGet, http.MethodPost}},
			struct {
				path    string
				methods []string
			}{path + "/{key}", []string{http.MethodGet, http.MethodPut}},
			struct {
				path    string
				methods []string
			}{path + "/{key}/versions", []string{http.MethodGet}},
			struct {
				path    string
				methods []string
			}{path + "/{key}/versions/{identifier}", []string{http.MethodGet}},
		)
		for _, action := range []string{"archive", "restore", "disable", "enable"} {
			routes = append(routes, struct {
				path    string
				methods []string
			}{path + "/{key}/" + action, []string{http.MethodPost}})
		}
	}
	for _, route := range routes {
		server.Route("").Handle(route.methods[0], route.path, agentJSONHandler(agent))
		for _, method := range route.methods[1:] {
			server.Route("").Handle(method, route.path, agentJSONHandler(agent))
		}
	}
}

func agentJSONHandler(agent *client.AgentClient) khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.AgentJSONOperation)
		handler := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveAgentJSON(requestCtx, ctx, agent)
		})
		_, err := handler(ctx, nil)
		return err
	}
}

func serveAgentJSON(ctx context.Context, httpCtx khttp.Context, agent *client.AgentClient) error {
	r := httpCtx.Request()
	if _, err := client.AgentOperation(r.Method, r.URL.Path); err != nil || r.URL.RawPath != "" || len(r.URL.RawQuery) > 2048 {
		return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "Invalid Agent path/query")
	}
	if !agent.Enabled() {
		return kerrors.New(503, "GATEWAY_AGENT_DISABLED", "Agent is not enabled")
	}
	if client.ValidateAgentContext(ctx) != nil {
		return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "Invalid request context")
	}
	rc, _ := gatewaymw.RequestContextFromContext(ctx)
	if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && !client.AgentAdministrator(rc.GetRoles()) {
		return kerrors.New(403, "GATEWAY_AGENT_FORBIDDEN", "Agent administration requires administrator")
	}
	if !agent.Acquire() {
		return kerrors.New(503, "GATEWAY_AGENT_CAPACITY", "Agent capacity is exhausted")
	}
	defer agent.Release()
	limits := agent.Limits()
	controller := http.NewResponseController(httpCtx.Response())
	deadline := time.Now().Add(limits.HeaderTimeout)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = controller.SetReadDeadline(deadline)
	body, err := io.ReadAll(http.MaxBytesReader(httpCtx.Response(), r.Body, int64(limits.MaxRequestBytes)))
	// 上传读取期限只约束请求体，后续上游响应由请求 context 控制。
	// 留在入站连接上会与 context 截止时间竞争，提前触发普通取消。
	_ = controller.SetReadDeadline(time.Time{})
	if err != nil {
		var maximum *http.MaxBytesError
		if errors.As(err, &maximum) {
			return kerrors.New(413, "GATEWAY_AGENT_REQUEST_TOO_LARGE", "Agent body exceeds limit")
		}
		return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "Cannot read request")
	}
	if r.Method == http.MethodGet {
		if len(body) != 0 {
			return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "GET body is not supported")
		}
	} else {
		id, parseErr := uuid.Parse(r.Header.Get("X-Request-ID"))
		if parseErr != nil || id.String() != r.Header.Get("X-Request-ID") {
			return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "X-Request-ID must be a canonical UUID")
		}
		media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaErr != nil || media != "application/json" || (r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity") {
			return kerrors.New(415, "GATEWAY_AGENT_UNSUPPORTED_MEDIA_TYPE", "Agent requires JSON")
		}
		if !utf8.Valid(body) || !json.Valid(body) {
			return kerrors.BadRequest("GATEWAY_AGENT_INVALID_ARGUMENT", "Invalid JSON")
		}
	}
	response, err := agent.JSON(ctx, r.Method, r.URL.Path, r.URL.RawQuery, body)
	if err != nil {
		if agentJSONTimeout(ctx, err) {
			return kerrors.New(504, "GATEWAY_AGENT_TIMEOUT", "Agent request timed out")
		}
		return kerrors.New(502, "GATEWAY_AGENT_UNAVAILABLE", "Agent unavailable")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAgentJSONBytes+1))
	if agentJSONTimeout(ctx, err) {
		return kerrors.New(504, "GATEWAY_AGENT_TIMEOUT", "Agent request timed out")
	}
	if err != nil || len(data) > maxAgentJSONBytes || !utf8.Valid(data) {
		return invalidAgentJSON()
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return invalidAgentJSON()
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(data, &payload) != nil || payload == nil {
		return invalidAgentJSON()
	}
	if response.StatusCode != 200 {
		var code string
		if json.Unmarshal(payload["code"], &code) != nil || !agentManagementErrors[code] ||
			(len(payload) != 1 && !(code == "AGENT_EVAL_DATASET_INVALID" && validEvalError(payload))) {
			return invalidAgentJSON()
		}
		switch response.StatusCode {
		case 400, 403, 404, 409, 413, 415, 503:
			return kerrors.New(response.StatusCode, code, "Agent could not complete the request")
		default:
			return invalidAgentJSON()
		}
	}
	if !validAgentJSONEnvelope(r.Method, r.URL.Path, payload) {
		return invalidAgentJSON()
	}
	httpCtx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	httpCtx.Response().Header().Set("Cache-Control", "no-store")
	_, err = httpCtx.Response().Write(data)
	return err
}

func invalidAgentJSON() error {
	return kerrors.New(502, "GATEWAY_AGENT_INVALID_RESPONSE", "Invalid Agent response")
}

func agentJSONTimeout(ctx context.Context, err error) bool {
	var timeout net.Error
	return errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded ||
		(errors.As(err, &timeout) && timeout.Timeout())
}

var agentManagementErrors = map[string]bool{
	"AGENT_RAG_NOT_FOUND": true, "AGENT_RAG_STALE": true, "AGENT_RAG_NOT_READY": true,
	"AGENT_RAG_REQUEST_CONFLICT": true, "AGENT_RAG_DEPENDENCY_UNAVAILABLE": true,
	"AGENT_CONVERSATION_NOT_FOUND": true,
	"AGENT_INVALID_ARGUMENT":       true, "AGENT_UNSUPPORTED_MEDIA_TYPE": true, "AGENT_REQUEST_TOO_LARGE": true,
	"AGENT_FORBIDDEN": true, "AGENT_UNAVAILABLE": true, "AGENT_MANAGEMENT_DISABLED": true,
	"AGENT_CONFIGURATION_NOT_FOUND": true, "AGENT_CONFIGURATION_UNSUPPORTED": true, "AGENT_CONFIGURATION_STALE": true,
	"AGENT_CONFIGURATION_REQUEST_CONFLICT": true, "AGENT_CONFIGURATION_REFERENCE_INVALID": true,
	"AGENT_CONFIGURATION_TOOL_INVALID": true, "AGENT_CONFIGURATION_RESTORE_INVALID": true,
	"AGENT_CONFIGURATION_TEST_EXPIRED": true, "AGENT_CREDENTIAL_UNAVAILABLE": true,
	"AGENT_MODEL_ENDPOINT_DENIED": true,
	"AGENT_EVAL_DATASET_INVALID":  true, "AGENT_EVAL_NOT_FOUND": true,
	"AGENT_EVAL_REVIEWER_INVALID":     true,
	"AGENT_EVAL_REVIEW_INVALID":       true,
	"AGENT_EVAL_REVIEW_INPUT_INVALID": true,
	"AGENT_EVAL_REVIEW_INTERRUPTED":   true,
	"AGENT_EVAL_CASE_NOT_COMPLETED":   true,
	"AGENT_EVAL_REVIEW_NOT_ENABLED":   true,
	"AGENT_EVAL_REVIEW_NOT_READY":     true,
	"AGENT_EVAL_UNAVAILABLE":          true,
	"AGENT_EVAL_VERSION_CONFLICT":     true,
	"AGENT_EVAL_HARD_FAILURE":         true,
	"AGENT_EVAL_REQUEST_CONFLICT":     true,
}

func validEvalError(payload map[string]json.RawMessage) bool {
	if len(payload) < 2 || len(payload) > 3 {
		return false
	}
	var message string
	if json.Unmarshal(payload["message"], &message) != nil || len(message) == 0 || len(message) > 256 {
		return false
	}
	if line, ok := payload["line"]; ok {
		var value int
		if json.Unmarshal(line, &value) != nil || value < 1 || value > 1001 {
			return false
		}
	}
	return true
}
