package biz

import (
	"testing"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
)

func TestSubmissionStateMachine(t *testing.T) {
	statuses := []submissionv1.SubmissionStatus{
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_UNSPECIFIED,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED,
	}
	want := map[[2]submissionv1.SubmissionStatus]bool{}
	for _, pair := range [][2]submissionv1.SubmissionStatus{
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED, submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED, submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED, submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING, submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT, submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT, submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT, submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED},
		{submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE, submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED},
	} {
		want[pair] = true
	}
	for _, from := range statuses {
		for _, to := range statuses {
			pair := [2]submissionv1.SubmissionStatus{from, to}
			if got := CanTransition(from, to); got != want[pair] {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want[pair])
			}
			if err := ValidateTransition(from, to); (err == nil) != want[pair] {
				t.Errorf("ValidateTransition(%s, %s) error = %v", from, to, err)
			}
		}
	}
}

func TestCanInvalidateEveryStatus(t *testing.T) {
	want := map[submissionv1.SubmissionStatus]bool{
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED:     true,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING:  true,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING:    true,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE:       true,
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT: true,
	}
	for value := submissionv1.SubmissionStatus(0); value <= submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED; value++ {
		if got := CanInvalidate(value); got != want[value] {
			t.Errorf("CanInvalidate(%s) = %v, want %v", value, got, want[value])
		}
	}
}
