package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	khttp "github.com/go-kratos/kratos/v3/transport/http"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
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
	authService *service.AuthService,
	_ *service.UserService,
) {
	server.HandleFunc("/api/v1/auth/register", authHandler(
		func() *gatewayv1.RegisterHTTPRequest { return &gatewayv1.RegisterHTTPRequest{} },
		authService.Register,
	))
	server.HandleFunc("/api/v1/auth/login", authHandler(
		func() *gatewayv1.LoginHTTPRequest { return &gatewayv1.LoginHTTPRequest{} },
		authService.Login,
	))
	server.HandleFunc("/api/v1/auth/refresh", authHandler(
		func() *gatewayv1.RefreshTokenHTTPRequest { return &gatewayv1.RefreshTokenHTTPRequest{} },
		authService.RefreshToken,
	))
}

func registerFutureRoutes(_ *service.ProblemService, _ *service.SubmissionService) {
	// Problem 和 Submission 先保留扩展点，等待对应服务完成后再接入。
}

type validatableRequest interface {
	Validate() error
}

func authHandler[Req validatableRequest, Resp any](
	newRequest func() Req,
	handle func(ctx context.Context, req Req) (Resp, error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := gatewaymw.RequestIDFromContext(r.Context())
		if r.Method != http.MethodPost {
			service.WriteError(w, service.ErrMethodNotAllowed(r.Method), requestID)
			return
		}

		req := newRequest()
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			service.WriteError(w, service.ErrInvalidJSON(err), requestID)
			return
		}
		if err := req.Validate(); err != nil {
			service.WriteError(w, service.ErrInvalidRequest(err), requestID)
			return
		}

		resp, err := handle(r.Context(), req)
		if err != nil {
			service.WriteError(w, err, requestID)
			return
		}
		service.WriteJSON(w, http.StatusOK, service.Envelope{
			Data:      resp,
			RequestID: requestID,
		})
	}
}
