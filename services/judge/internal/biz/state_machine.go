package biz

import submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"

var allowedTransitions = map[submissionv1.SubmissionStatus]map[submissionv1.SubmissionStatus]struct{}{
	submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED: {
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING:   {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED:   {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED: {},
	},
	submissionv1.SubmissionStatus_SUBMISSION_STATUS_COMPILING: {
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING:     {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE:        {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT:  {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED:   {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED: {},
	},
	submissionv1.SubmissionStatus_SUBMISSION_STATUS_RUNNING: {
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE:        {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT:  {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED:   {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED: {},
	},
	submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT: {
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED:      {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED:   {},
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED: {},
	},
	submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE: {
		submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED: {},
	},
}

func CanTransition(from, to submissionv1.SubmissionStatus) bool {
	_, ok := allowedTransitions[from][to]
	return ok
}

func ValidateTransition(from, to submissionv1.SubmissionStatus) error {
	if !CanTransition(from, to) {
		return ErrorInvalidTransition("submission cannot transition from %s to %s", from.String(), to.String())
	}
	return nil
}

func CanInvalidate(status submissionv1.SubmissionStatus) bool {
	return CanTransition(status, submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED)
}
