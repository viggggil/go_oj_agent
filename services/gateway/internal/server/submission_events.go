package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func registerSubmissionEventsRoute(server *khttp.Server, gateway *service.GatewayService, interval, maxDuration time.Duration) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if maxDuration <= 0 {
		maxDuration = 2 * time.Minute
	}
	server.Route("").GET("/api/v1/submissions/{submission_id}/events", func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, gatewayv1.OperationGatewayServiceGetJudgeResult)
		handler := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveSubmissionEvents(requestCtx, ctx, gateway, interval, maxDuration)
		})
		_, err := handler(ctx, nil)
		return err
	})
}

func serveSubmissionEvents(requestCtx context.Context, ctx khttp.Context, gateway *service.GatewayService, interval, maxDuration time.Duration) error {
	id, err := strconv.ParseInt(ctx.Vars().Get("submission_id"), 10, 64)
	if err != nil || id <= 0 {
		return kerrors.BadRequest("GATEWAY_INVALID_ARGUMENT", "submission_id must be a positive integer")
	}
	requestCtx, cancel := context.WithTimeout(requestCtx, maxDuration)
	defer cancel()
	w := ctx.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flush, _ := w.(http.Flusher)
	var last string
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		result, callErr := gateway.GetJudgeResult(requestCtx, &gatewayv1.GetJudgeResultRequest{SubmissionId: id})
		if callErr != nil {
			_, _ = fmt.Fprintf(w, "event: submission.error\ndata: %q\n\n", callErr.Error())
			if flush != nil {
				flush.Flush()
			}
			return nil
		}
		data, _ := json.Marshal(result.GetResult())
		state := string(data)
		if state != last {
			event := "updated"
			if last == "" {
				event = "snapshot"
			}
			_, _ = fmt.Fprintf(w, "event: submission.%s\ndata: %s\n\n", event, data)
			if flush != nil {
				flush.Flush()
			}
			last = state
		}
		if terminalSubmissionStatus(result.GetResult().GetStatus()) {
			return nil
		}
		select {
		case <-requestCtx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func terminalSubmissionStatus(status submissionv1.SubmissionStatus) bool {
	value := status.String()
	return value == "SUBMISSION_STATUS_DONE" || value == "SUBMISSION_STATUS_CANCELLED" || value == "SUBMISSION_STATUS_INVALIDATED"
}
