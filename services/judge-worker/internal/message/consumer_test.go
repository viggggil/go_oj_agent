package message

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
)

type fakeDelivery struct {
	body              []byte
	ack, nack, reject bool
	requeue           bool
}

func (d *fakeDelivery) GetBody() []byte { return d.body }
func (d *fakeDelivery) Ack(bool) error  { d.ack = true; return nil }
func (d *fakeDelivery) Nack(_ bool, requeue bool) error {
	d.nack = true
	d.requeue = requeue
	return nil
}
func (d *fakeDelivery) Reject(_ bool) error { d.reject = true; return nil }

type fakeEngine struct {
	outcome biz.Outcome
	calls   int
}

func (e *fakeEngine) Execute(context.Context, mq.JudgeTask) biz.Outcome { e.calls++; return e.outcome }

type fakeReporter struct {
	err               error
	completed, failed int
	retries           []mq.JudgeTask
}

func (r *fakeReporter) ReportCompleted(context.Context, mq.Envelope, mq.JudgeCompleted) error {
	r.completed++
	return r.err
}
func (r *fakeReporter) ReportFailed(context.Context, mq.Envelope, mq.JudgeFailed) error {
	r.failed++
	return r.err
}
func (r *fakeReporter) ReportRetry(_ context.Context, _ mq.Envelope, task mq.JudgeTask) error {
	r.retries = append(r.retries, task)
	return r.err
}

func validBody(t *testing.T) []byte {
	t.Helper()
	task := mq.JudgeTask{SubmissionID: 1, ProblemID: 2, Language: "go", JudgeRevision: "12345678901234567890123456", SourceObjectKey: "x.go", SourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", SourceSizeBytes: 1, JudgeDeadlineAt: time.Now().Add(time.Minute)}
	body, err := mq.MarshalEnvelope(mq.EnvelopeMetadata{EventID: uuid.NewString(), EventType: mq.EventTypeJudgeTask, EventVersion: mq.EventVersion1, OccurredAt: time.Now()}, task)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDecodeTaskRejectsMalformed(t *testing.T) {
	if _, _, err := DecodeTask([]byte("{")); err == nil {
		t.Fatal("expected malformed JSON error")
	}
	body := validBody(t)
	body[len(body)-2] = 'x'
	if _, _, err := DecodeTask(body); err == nil {
		t.Fatal("expected malformed payload error")
	}
}

func TestHandleAckAfterConfirmedResult(t *testing.T) {
	engine := &fakeEngine{outcome: biz.Outcome{Completed: &mq.JudgeCompleted{SubmissionID: 1, JudgeRevision: "12345678901234567890123456", Verdict: "AC"}}}
	reporter := &fakeReporter{}
	d := &fakeDelivery{body: validBody(t)}
	(&Consumer{Engine: engine, Reporter: reporter, TaskTimeout: time.Second}).Handle(context.Background(), d)
	if !d.ack || d.nack || d.reject {
		t.Fatalf("unexpected delivery state: %+v", d)
	}
	if engine.calls != 1 || reporter.completed != 1 {
		t.Fatalf("engine/reporter calls: %d/%d", engine.calls, reporter.completed)
	}
}

func TestHandleRequeuesOnPublishFailure(t *testing.T) {
	engine := &fakeEngine{outcome: biz.Outcome{Failed: &mq.JudgeFailed{SubmissionID: 1, JudgeRevision: "12345678901234567890123456", Reason: "TEMP", Retryable: true}}}
	reporter := &fakeReporter{err: context.DeadlineExceeded}
	d := &fakeDelivery{body: validBody(t)}
	(&Consumer{Engine: engine, Reporter: reporter, TaskTimeout: time.Second}).Handle(context.Background(), d)
	if d.ack || !d.nack || !d.requeue || d.reject {
		t.Fatalf("unexpected delivery state: %+v", d)
	}
}

func TestHandleRetryPublishesAttemptAndAcksAfterConfirm(t *testing.T) {
	engine := &fakeEngine{outcome: biz.Outcome{Failed: &mq.JudgeFailed{SubmissionID: 1, JudgeRevision: "12345678901234567890123456", Code: mq.FailureSandboxUnavailable, Message: "temporary", Retryable: true}}}
	reporter := &fakeReporter{}
	d := &fakeDelivery{body: validBody(t)}
	(&Consumer{Engine: engine, Reporter: reporter, TaskTimeout: time.Second, MaxRetries: 3, RetryDelay: time.Second}).Handle(context.Background(), d)
	if !d.ack || d.nack || d.reject || len(reporter.retries) != 1 || reporter.retries[0].Attempt != 1 {
		t.Fatalf("unexpected retry state: delivery=%+v retries=%+v", d, reporter.retries)
	}
}

func TestHandleMaxRetryReportsTerminalFailure(t *testing.T) {
	engine := &fakeEngine{outcome: biz.Outcome{Failed: &mq.JudgeFailed{SubmissionID: 1, JudgeRevision: "12345678901234567890123456", Code: mq.FailureSandboxUnavailable, Message: "temporary", Retryable: true}}}
	reporter := &fakeReporter{}
	taskBody := validBody(t)
	var envelope mq.Envelope
	var task mq.JudgeTask
	var err error
	envelope, err = mq.UnmarshalEnvelope(taskBody, mq.EventTypeJudgeTask, &task)
	if err != nil {
		t.Fatal(err)
	}
	task.Attempt = 3
	taskBody, err = mq.MarshalEnvelope(mq.EnvelopeMetadata{EventID: envelope.EventID, EventType: envelope.EventType, EventVersion: envelope.EventVersion, OccurredAt: envelope.OccurredAt}, task)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDelivery{body: taskBody}
	(&Consumer{Engine: engine, Reporter: reporter, TaskTimeout: time.Second, MaxRetries: 3, RetryDelay: time.Second}).Handle(context.Background(), d)
	if !d.ack || d.nack || reporter.failed != 1 || len(reporter.retries) != 0 {
		t.Fatalf("unexpected terminal retry state: delivery=%+v failed=%d retries=%d", d, reporter.failed, len(reporter.retries))
	}
}

func TestHandleExpiredTaskDoesNotInvokeEngine(t *testing.T) {
	engine := &fakeEngine{outcome: biz.Outcome{Completed: &mq.JudgeCompleted{SubmissionID: 1, JudgeRevision: "12345678901234567890123456", Verdict: "AC"}}}
	reporter := &fakeReporter{}
	taskBody := validBody(t)
	var envelope mq.Envelope
	var task mq.JudgeTask
	var err error
	envelope, err = mq.UnmarshalEnvelope(taskBody, mq.EventTypeJudgeTask, &task)
	if err != nil {
		t.Fatal(err)
	}
	task.JudgeDeadlineAt = time.Now().Add(-time.Second)
	taskBody, err = mq.MarshalEnvelope(mq.EnvelopeMetadata{EventID: envelope.EventID, EventType: envelope.EventType, EventVersion: envelope.EventVersion, OccurredAt: envelope.OccurredAt}, task)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDelivery{body: taskBody}
	(&Consumer{Engine: engine, Reporter: reporter, TaskTimeout: time.Second}).Handle(context.Background(), d)
	if engine.calls != 0 || !d.ack || reporter.failed != 1 {
		t.Fatalf("expired task state: calls=%d delivery=%+v failed=%d", engine.calls, d, reporter.failed)
	}
}

func TestPublisherReporterRetryCarriesCausationAndTrace(t *testing.T) {
	publisher := &recordingPublisher{}
	reporter := NewReporter(publisher)
	input := mq.Envelope{EventID: uuid.NewString(), EventType: mq.EventTypeJudgeTask, EventVersion: mq.EventVersion1, OccurredAt: time.Now().UTC(), TraceID: "trace-1"}
	task := mq.JudgeTask{SubmissionID: 1, ProblemID: 2, Language: "go", JudgeRevision: "12345678901234567890123456", SourceObjectKey: "x.go", SourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", SourceSizeBytes: 1, JudgeDeadlineAt: time.Now().Add(time.Minute), Attempt: 1}
	if err := reporter.ReportRetry(context.Background(), input, task); err != nil {
		t.Fatal(err)
	}
	var envelope mq.Envelope
	var got mq.JudgeTask
	if _, err := mq.UnmarshalEnvelope(publisher.body, mq.EventTypeJudgeTask, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(publisher.body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.CausationID != input.EventID || envelope.TraceID != input.TraceID || envelope.EventID == input.EventID || got.Attempt != 1 {
		t.Fatalf("retry envelope=%+v task=%+v", envelope, got)
	}
}

type recordingPublisher struct{ body []byte }

func (p *recordingPublisher) Publish(_ context.Context, _ string, _ string, body []byte) error {
	p.body = body
	return nil
}

func TestRunDeliveryWorkersDrainsInFlightDeliveryBeforeDone(t *testing.T) {
	deliveries := make(chan amqp091.Delivery, 1)
	deliveries <- amqp091.Delivery{Body: []byte("in-flight")}
	close(deliveries)
	started := make(chan struct{})
	release := make(chan struct{})
	done := runDeliveryWorkers(deliveries, 1, func(amqp091.Delivery) {
		close(started)
		<-release
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not receive delivery")
	}
	select {
	case <-done:
		t.Fatal("worker completed before in-flight delivery was released")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not drain in-flight delivery")
	}
}
