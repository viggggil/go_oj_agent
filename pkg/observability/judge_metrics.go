package observability

import (
	"context"
	"sync"
	"time"

	otelmetric "go.opentelemetry.io/otel/metric"
)

var (
	judgeMetricsOnce sync.Once
	judgeMetrics     JudgeMetrics
)

// Judge returns the process-wide Judge/Worker instrument set.
func Judge() JudgeMetrics {
	judgeMetricsOnce.Do(func() { judgeMetrics = NewJudgeMetrics(Meter()) })
	return judgeMetrics
}

// JudgeMetrics contains the low-cardinality instruments shared by the Judge
// service, relay, result consumer and Worker. Recording is deliberately
// best-effort; no instrument error can affect the business path.
type JudgeMetrics struct {
	SubmissionTotal           otelmetric.Int64Counter
	ResultTotal               otelmetric.Int64Counter
	ResultPublishFailureTotal otelmetric.Int64Counter
	QueueDuration             otelmetric.Float64Histogram
	CompileDuration           otelmetric.Float64Histogram
	ExecuteDuration           otelmetric.Float64Histogram
	OutboxPublishTotal        otelmetric.Int64Counter
	OutboxRetryTotal          otelmetric.Int64Counter
	OutboxDLQTotal            otelmetric.Int64Counter
	WorkerTaskTotal           otelmetric.Int64Counter
	WorkerRetryTotal          otelmetric.Int64Counter
	WorkerInFlight            otelmetric.Int64UpDownCounter
	WorkerTaskDuration        otelmetric.Float64Histogram
	SandboxDuration           otelmetric.Float64Histogram
	ResultConsumeTotal        otelmetric.Int64Counter
}

func NewJudgeMetrics(meter otelmetric.Meter) JudgeMetrics {
	if meter == nil {
		return JudgeMetrics{}
	}
	counter := func(name, description string) otelmetric.Int64Counter {
		value, _ := meter.Int64Counter(name, otelmetric.WithDescription(description), otelmetric.WithUnit("1"))
		return value
	}
	histogram := func(name, description string) otelmetric.Float64Histogram {
		value, _ := meter.Float64Histogram(name, otelmetric.WithDescription(description), otelmetric.WithUnit("s"))
		return value
	}
	workerInFlight, _ := meter.Int64UpDownCounter("oj.judge.worker.in_flight", otelmetric.WithDescription("Judge worker tasks currently executing"), otelmetric.WithUnit("1"))
	return JudgeMetrics{
		SubmissionTotal:           counter("oj.judge.submission.total", "Judge submissions accepted by the Judge service"),
		ResultTotal:               counter("oj.judge.result.total", "Judge results durably produced"),
		ResultPublishFailureTotal: counter("oj.judge.result.publish.failure.total", "Judge result publications not confirmed by the broker"),
		QueueDuration:             histogram("oj.judge.queue.duration", "Time from submission creation to worker task start"),
		CompileDuration:           histogram("oj.judge.compile.duration", "Judge compilation duration"),
		ExecuteDuration:           histogram("oj.judge.execute.duration", "Judge execution duration"),
		OutboxPublishTotal:        counter("oj.judge.outbox.publish.total", "Judge Outbox publish outcomes"),
		OutboxRetryTotal:          counter("oj.judge.outbox.retry.total", "Judge Outbox publish retries"),
		OutboxDLQTotal:            counter("oj.judge.dlq.total", "Judge messages sent to a dead-letter path"),
		WorkerTaskTotal:           counter("oj.judge.worker.task.total", "Judge worker task outcomes"),
		WorkerRetryTotal:          counter("oj.judge.worker.retry.total", "Confirmed delayed judge task retries"),
		WorkerInFlight:            workerInFlight,
		WorkerTaskDuration:        histogram("oj.judge.worker.task.duration", "Judge worker task duration"),
		SandboxDuration:           histogram("oj.judge.worker.sandbox.duration", "Judge sandbox compile and execution duration"),
		ResultConsumeTotal:        counter("oj.judge.result.consume.total", "Judge result consumer outcomes"),
	}
}

func (m JudgeMetrics) add(counter otelmetric.Int64Counter, ctx context.Context, labels ...Label) {
	if counter != nil {
		counter.Add(ctx, 1, otelmetric.WithAttributes(Attributes(labels...)...))
	}
}

func (m JudgeMetrics) record(histogram otelmetric.Float64Histogram, ctx context.Context, duration time.Duration, labels ...Label) {
	if histogram != nil && duration >= 0 {
		histogram.Record(ctx, duration.Seconds(), otelmetric.WithAttributes(Attributes(labels...)...))
	}
}

func (m JudgeMetrics) Submission(ctx context.Context, language, outcome string) {
	m.add(m.SubmissionTotal, ctx, Label{Key: LabelLanguage, Value: language}, Label{Key: LabelOutcome, Value: outcome})
}

func (m JudgeMetrics) Result(ctx context.Context, verdict, outcome string) {
	m.add(m.ResultTotal, ctx, Label{Key: LabelVerdict, Value: verdict}, Label{Key: LabelOutcome, Value: outcome})
}

func (m JudgeMetrics) ResultPublishFailed(ctx context.Context) {
	m.add(m.ResultPublishFailureTotal, ctx, Label{Key: LabelOutcome, Value: "error"})
}

func (m JudgeMetrics) Queue(ctx context.Context, duration time.Duration) {
	m.record(m.QueueDuration, ctx, duration)
}

func (m JudgeMetrics) Compile(ctx context.Context, duration time.Duration, outcome string) {
	m.record(m.CompileDuration, ctx, duration, Label{Key: LabelOutcome, Value: outcome})
}

func (m JudgeMetrics) Execute(ctx context.Context, duration time.Duration, language, verdict string) {
	m.record(m.ExecuteDuration, ctx, duration, Label{Key: LabelLanguage, Value: language}, Label{Key: LabelVerdict, Value: verdict})
}

func (m JudgeMetrics) OutboxPublished(ctx context.Context, outcome string) {
	m.add(m.OutboxPublishTotal, ctx, Label{Key: LabelOutcome, Value: outcome})
}

func (m JudgeMetrics) OutboxRetried(ctx context.Context, outcome string) {
	m.add(m.OutboxRetryTotal, ctx, Label{Key: LabelOutcome, Value: outcome})
}

func (m JudgeMetrics) OutboxDead(ctx context.Context, errorCode string) {
	m.add(m.OutboxDLQTotal, ctx, Label{Key: LabelErrorCode, Value: errorCode})
}

func (m JudgeMetrics) WorkerTask(ctx context.Context, language, outcome string) {
	m.add(m.WorkerTaskTotal, ctx, Label{Key: LabelLanguage, Value: language}, Label{Key: LabelOutcome, Value: outcome})
}

func (m JudgeMetrics) WorkerRetry(ctx context.Context, language string) {
	m.add(m.WorkerRetryTotal, ctx, Label{Key: LabelLanguage, Value: language})
}

func (m JudgeMetrics) WorkerStart(ctx context.Context) {
	if m.WorkerInFlight != nil {
		m.WorkerInFlight.Add(ctx, 1)
	}
}

func (m JudgeMetrics) WorkerStop(ctx context.Context) {
	if m.WorkerInFlight != nil {
		m.WorkerInFlight.Add(ctx, -1)
	}
}

func (m JudgeMetrics) WorkerInFlightChange(ctx context.Context, delta int64) {
	if m.WorkerInFlight != nil && delta != 0 {
		m.WorkerInFlight.Add(ctx, delta)
	}
}

func (m JudgeMetrics) WorkerDuration(ctx context.Context, duration time.Duration, language, outcome string) {
	m.record(m.WorkerTaskDuration, ctx, duration, Label{Key: LabelLanguage, Value: language}, Label{Key: LabelOutcome, Value: outcome})
}

func (m JudgeMetrics) Sandbox(ctx context.Context, duration time.Duration, language, phase string) {
	m.record(m.SandboxDuration, ctx, duration, Label{Key: LabelLanguage, Value: language}, Label{Key: LabelPhase, Value: phase})
}

func (m JudgeMetrics) ResultConsumed(ctx context.Context, outcome string) {
	m.add(m.ResultConsumeTotal, ctx, Label{Key: LabelOutcome, Value: outcome})
}
