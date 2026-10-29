package server

import (
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/middleware/validate"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func NewHTTPServer(
	config *conf.Bootstrap,
	authMiddleware *gatewaymw.AuthMiddleware,
	gatewayService *service.GatewayService,
) *khttp.Server {
	return NewHTTPServerWithAgent(config, authMiddleware, gatewayService, nil)
}

func NewHTTPServerWithAgent(config *conf.Bootstrap, authMiddleware *gatewaymw.AuthMiddleware, gatewayService *service.GatewayService, agent *client.AgentClient) *khttp.Server {
	address := ":8080"
	timeout := 3 * time.Second
	sseInterval := 500 * time.Millisecond
	sseMaxDuration := 2 * time.Minute
	if config != nil && config.GetServer() != nil && config.GetServer().GetHttp() != nil {
		if config.GetServer().GetHttp().GetAddress() != "" {
			address = config.GetServer().GetHttp().GetAddress()
		}
		if parsed, err := time.ParseDuration(config.GetServer().GetHttp().GetTimeout()); err == nil && parsed > 0 {
			timeout = parsed
		}
		if config.GetServer().GetSse() != nil {
			if parsed, err := time.ParseDuration(config.GetServer().GetSse().GetPollInterval()); err == nil && parsed > 0 {
				sseInterval = parsed
			}
			if parsed, err := time.ParseDuration(config.GetServer().GetSse().GetMaxDuration()); err == nil && parsed > 0 {
				sseMaxDuration = parsed
			}
		}
	}

	server := khttp.NewServer(
		khttp.Address(address),
		khttp.Timeout(0),
		khttp.Filter(func(next http.Handler) http.Handler {
			budgeted := requestBudgetFilter(timeout, agent.Limits().MaxDuration)(next)
			return otelhttp.NewHandler(ignoreExternalTraceContext(budgeted), "gateway.http")
		}),
		khttp.Middleware(
			gatewaymw.RequestIDMiddleware(),
			recovery.Recovery(),
			validate.Validator(),
		),
	)
	gatewayv1.RegisterGatewayServiceHTTPServer(server, gatewayService)
	registerProblemUploadRoute(server, gatewayService)
	registerSubmissionEventsRoute(server, gatewayService, sseInterval, sseMaxDuration)
	registerJudgeObservabilityRoute(server, gatewayService)
	registerMetricsObservabilityRoute(server)
	registerLogsObservabilityRoute(server)
	registerAgentChatRoute(server, agent)
	registerAgentManagementRoutes(server, agent)
	server.Use(client.AgentChatOperation, authMiddleware.Middleware())
	server.Use(client.AgentJSONOperation, authMiddleware.Middleware())
	server.Use(client.JudgeObservabilityOperation, authMiddleware.Middleware())
	server.Use(client.MetricsObservabilityOperation, authMiddleware.Middleware())
	server.ReadHeaderTimeout = 5 * time.Second
	server.IdleTimeout = time.Minute
	server.Use(
		gatewayv1.OperationGatewayServiceGetCurrentUser,
		authMiddleware.Middleware(),
	)
	server.Use(
		gatewayv1.OperationGatewayServiceGetUser,
		authMiddleware.Middleware(),
	)
	for _, operation := range []string{
		gatewayv1.OperationGatewayServiceCreateProblem,
		gatewayv1.OperationGatewayServiceGetProblem,
		gatewayv1.OperationGatewayServiceListProblems,
		gatewayv1.OperationGatewayServiceUpdateProblem,
		gatewayv1.OperationGatewayServiceArchiveProblem,
		gatewayv1.OperationGatewayServiceAddTestcase,
		gatewayv1.OperationGatewayServiceListProblemTestcases,
		gatewayv1.OperationGatewayServiceArchiveTestcase,
		gatewayv1.OperationGatewayServiceListContests,
		gatewayv1.OperationGatewayServiceGetContest,
		gatewayv1.OperationGatewayServiceCreateContest,
		gatewayv1.OperationGatewayServiceUpdateContest,
		gatewayv1.OperationGatewayServiceArchiveContest,
		gatewayv1.OperationGatewayServiceGetContestLeaderboard,
		gatewayv1.OperationGatewayServiceCreateSubmission,
		gatewayv1.OperationGatewayServiceGetSubmission,
		gatewayv1.OperationGatewayServiceGetSubmissionSource,
		gatewayv1.OperationGatewayServiceGetJudgeResult,
		gatewayv1.OperationGatewayServiceListSubmissions,
		gatewayv1.OperationGatewayServiceRejudgeSubmission,
		gatewayv1.OperationGatewayServiceJoinContest,
		gatewayv1.OperationGatewayServiceCreateContestSubmission,
	} {
		server.Use(operation, authMiddleware.Middleware())
	}
	return server
}

// The Gateway is the trust boundary: public trace context must not select its root trace.
func ignoreExternalTraceContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.Clone(r.Context())
		request.Header = r.Header.Clone()
		request.Header.Del("traceparent")
		request.Header.Del("tracestate")
		next.ServeHTTP(w, request)
	})
}
