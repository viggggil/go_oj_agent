package integration_test

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

func TestRabbitRetryTTLReturnsTaskAndPreservesAttempt(t *testing.T) {
	url := os.Getenv("RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("set RABBITMQ_TEST_URL")
	}
	conn, err := amqp091.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	exchange := "worker.retry.integration." + uuid.NewString()
	retryQueue := exchange + ".retry"
	taskQueue := exchange + ".task"
	if err = ch.ExchangeDeclare(exchange, amqp091.ExchangeTopic, true, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ch.QueueDeclare(taskQueue, true, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err = ch.QueueBind(taskQueue, mq.RoutingJudgeTaskGo, exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ch.QueueDeclare(retryQueue, true, true, false, false, amqp091.Table{"x-message-ttl": int32(100), "x-dead-letter-exchange": exchange, "x-dead-letter-routing-key": mq.RoutingJudgeTaskGo}); err != nil {
		t.Fatal(err)
	}
	if err = ch.QueueBind(retryQueue, "judge.retry.go", exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	task := mq.JudgeTask{SubmissionID: 1, ProblemID: 2, Language: "go", JudgeRevision: "12345678901234567890123456", SourceObjectKey: "sources/x/source.go", SourceSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", SourceSizeBytes: 1, JudgeDeadlineAt: time.Now().Add(time.Minute), Attempt: 1}
	body, err := mq.MarshalEnvelope(mq.EnvelopeMetadata{EventID: uuid.NewString(), EventType: mq.EventTypeJudgeTask, EventVersion: mq.EventVersion1, OccurredAt: time.Now().UTC()}, task)
	if err != nil {
		t.Fatal(err)
	}
	if err = ch.Publish(exchange, "judge.retry.go", false, false, amqp091.Publishing{Body: body, DeliveryMode: amqp091.Persistent}); err != nil {
		t.Fatal(err)
	}
	deliveries, err := ch.Consume(taskQueue, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case delivery := <-deliveries:
		var got mq.JudgeTask
		if _, err = mq.UnmarshalEnvelope(delivery.Body, mq.EventTypeJudgeTask, &got); err != nil {
			t.Fatal(err)
		}
		if got.Attempt != 1 {
			t.Fatalf("attempt = %d, want 1", got.Attempt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retry task did not return after TTL")
	}
}
