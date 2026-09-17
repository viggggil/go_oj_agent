package service

import (
	"context"
	"testing"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBootstrapHandlersRemainUnimplemented(t *testing.T) {
	service := NewSubmissionService()
	_, err := service.CreateSubmission(context.Background(), &submissionv1.CreateSubmissionRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("CreateSubmission status = %v, want %v", status.Code(err), codes.Unimplemented)
	}
}
