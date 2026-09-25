package message

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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
}

func (r *fakeReporter) ReportCompleted(context.Context, mq.Envelope, mq.JudgeCompleted) error {
	r.completed++
	return r.err
}
func (r *fakeReporter) ReportFailed(context.Context, mq.Envelope, mq.JudgeFailed) error {
	r.failed++
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
