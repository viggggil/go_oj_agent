package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
	"google.golang.org/grpc"
)

type contestHTTPClient struct {
	contestv1.ContestServiceClient
	userID int64
}

func (c *contestHTTPClient) CreateContestSubmission(ctx context.Context, req *contestv1.CreateContestSubmissionRequest, _ ...grpc.CallOption) (*contestv1.CreateContestSubmissionReply, error) {
	rc, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok {
		return nil, gatewaymw.ErrUnauthenticated("missing request context")
	}
	c.userID = rc.GetUserId()
	if req.GetContestId() != 2 || req.GetProblemId() != 7 {
		return nil, gatewaymw.ErrUnauthenticated("wrong path bindings")
	}
	return &contestv1.CreateContestSubmissionReply{SubmissionId: 9}, nil
}

func TestContestSubmissionHTTPAuthentication(t *testing.T) {
	auth, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	client := &contestHTTPClient{}
	svc := service.NewGatewayServiceWithAllClients(nil, nil, nil, nil, client)
	server := NewHTTPServer(testConfig(), auth, svc)
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/contests/2/problems/7/submissions", strings.NewReader(`{"language":"go","source_code":"package main","idempotency_key":"123e4567-e89b-12d3-a456-426614174000"}`))
		request.Header.Set("Content-Type", "application/json")
		if authenticated {
			request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		expected := http.StatusUnauthorized
		if authenticated {
			expected = http.StatusOK
		}
		if response.Code != expected {
			t.Fatalf("authenticated=%v status=%d body=%s", authenticated, response.Code, response.Body.String())
		}
	}
	if client.userID != 1001 {
		t.Fatalf("actor=%d", client.userID)
	}
}
