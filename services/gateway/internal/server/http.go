package server

import (
	"net/http"
	"time"

	khttp "github.com/go-kratos/kratos/v3/transport/http"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func NewHTTPServer(
	config *conf.Bootstrap,
	authMiddleware *gatewaymw.AuthMiddleware,
	authService *service.AuthService,
	userService *service.UserService,
	problemService *service.ProblemService,
	submissionService *service.SubmissionService,
) *khttp.Server {
	address := ":8080"
	timeout := 3 * time.Second
	if config != nil && config.GetServer() != nil && config.GetServer().GetHttp() != nil {
		if config.GetServer().GetHttp().GetAddress() != "" {
			address = config.GetServer().GetHttp().GetAddress()
		}
		if parsed, err := time.ParseDuration(config.GetServer().GetHttp().GetTimeout()); err == nil && parsed > 0 {
			timeout = parsed
		}
	}

	server := khttp.NewServer(
		khttp.Address(address),
		khttp.Timeout(timeout),
		khttp.Filter(gatewaymw.RequestIDFilter()),
	)
	registerHealthRoute(server)
	registerUserRoutes(server, authMiddleware, authService, userService)
	registerFutureRoutes(problemService, submissionService)
	return server
}

func registerHealthRoute(server *khttp.Server) {
	server.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		service.WriteJSON(w, http.StatusOK, service.Envelope{
			Data:      map[string]string{"status": "ok"},
			RequestID: gatewaymw.RequestIDFromContext(r.Context()),
		})
	})
}

func registerUserRoutes(
	server *khttp.Server,
	_ *gatewaymw.AuthMiddleware,
	_ *service.AuthService,
	_ *service.UserService,
) {
	// 后续 PR 在这里注册 /api/v1/auth/* 和 /api/v1/users/*。
}

func registerFutureRoutes(_ *service.ProblemService, _ *service.SubmissionService) {
	// Problem 和 Submission 先保留扩展点，等待对应服务完成后再接入。
}
