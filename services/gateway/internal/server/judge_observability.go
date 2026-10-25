package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

const judgeTracePath = "/api/v1/admin/observability/judge/submissions/{submission_id:[0-9]+}"

type judgeTraceResponse struct {
	SubmissionID  int64  `json:"submission_id"`
	TempoURL      string `json:"tempo_url"`
	LinkAvailable bool   `json:"link_available"`
	// TraceAvailable deliberately remains false: a search link does not prove
	// that the trace was sampled or retained in Tempo.
	TraceAvailable bool   `json:"trace_available"`
	Status         string `json:"status"`
}

func registerJudgeObservabilityRoute(server *khttp.Server, gateway *service.GatewayService) {
	server.Route("").GET(judgeTracePath, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.JudgeObservabilityOperation)
		handler := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveJudgeTrace(requestCtx, ctx, gateway)
		})
		_, err := handler(ctx, nil)
		return err
	})
}

func serveJudgeTrace(requestCtx context.Context, httpCtx khttp.Context, gateway *service.GatewayService) error {
	rc, ok := gatewaymw.RequestContextFromContext(requestCtx)
	if !ok || !platformAdministrator(rc.GetRoles()) {
		return kerrors.New(http.StatusForbidden, "GATEWAY_OBSERVABILITY_FORBIDDEN", "Judge trace requires platform administrator")
	}
	id, err := strconv.ParseInt(httpCtx.Vars().Get("submission_id"), 10, 64)
	if err != nil || id <= 0 {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "submission_id must be a positive integer")
	}
	owner := gateway.SubmissionClient()
	if owner == nil {
		return kerrors.New(http.StatusServiceUnavailable, "GATEWAY_OBSERVABILITY_UNAVAILABLE", "Judge service is not configured")
	}
	lookup, err := owner.GetSubmission(requestCtx, &submissionv1.GetSubmissionRequest{SubmissionId: id})
	if err != nil {
		// Preserve the owning service's authorization and not-found boundary.
		return err
	}
	if lookup == nil || lookup.GetSubmission() == nil || lookup.GetSubmission().GetId() != id {
		return kerrors.New(http.StatusBadGateway, "GATEWAY_OBSERVABILITY_INVALID_JUDGE_RESPONSE", "Judge service returned an invalid submission")
	}
	response := buildJudgeTraceResponse(id, os.Getenv("JUDGE_OBSERVABILITY_GRAFANA_URL"))
	httpCtx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	httpCtx.Response().Header().Set("Cache-Control", "no-store")
	return json.NewEncoder(httpCtx.Response()).Encode(response)
}

func platformAdministrator(roles []string) bool {
	for _, role := range roles {
		if role == "admin" || role == "system_admin" {
			return true
		}
	}
	return false
}

func buildJudgeTraceResponse(submissionID int64, configured string) judgeTraceResponse {
	response := judgeTraceResponse{SubmissionID: submissionID, Status: "NOT_CONFIGURED"}
	base, err := url.Parse(strings.TrimSpace(configured))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return response
	}
	query := `{ resource.service.name =~ "gateway-service|judge-service|judge-worker|contest-service" && span.submission.id = ` + strconv.FormatInt(submissionID, 10) + ` }`
	left, _ := json.Marshal(map[string]any{
		"datasource": "tempo",
		"queries":    []map[string]string{{"queryType": "traceql", "query": query, "refId": "A"}},
		"range":      map[string]string{"from": "now-24h", "to": "now"},
	})
	path := strings.TrimRight(base.Path, "/") + "/explore"
	base.Path, base.RawPath = path, ""
	base.Fragment = ""
	values := url.Values{}
	values.Set("orgId", "1")
	values.Set("left", string(left))
	base.RawQuery = values.Encode()
	response.TempoURL = base.String()
	response.LinkAvailable = true
	response.Status = "SEARCHABLE"
	return response
}
