package integration_test

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

func TestRabbitMalformedTaskGoesToDLQ(t *testing.T) {
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
	exchange := "worker.integration." + uuid.NewString()
	taskQueue := exchange + ".task"
	dlq := exchange + ".dlq"
	if err = ch.ExchangeDeclare(exchange, amqp091.ExchangeTopic, true, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ch.QueueDeclare(dlq, true, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err = ch.QueueBind(dlq, dlq, exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ch.QueueDeclare(taskQueue, true, true, false, false, amqp091.Table{"x-dead-letter-exchange": exchange, "x-dead-letter-routing-key": dlq}); err != nil {
		t.Fatal(err)
	}
	if err = ch.QueueBind(taskQueue, mq.RoutingJudgeTaskGo, exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	deliveries, err := ch.Consume(taskQueue, "", false, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ch.Publish(exchange, mq.RoutingJudgeTaskGo, false, false, amqp091.Publishing{Body: []byte("{"), DeliveryMode: amqp091.Persistent}); err != nil {
		t.Fatal(err)
	}
	select {
	case delivery := <-deliveries:
		if err = delivery.Reject(false); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out consuming malformed task")
	}
	dlqDeliveries, err := ch.Consume(dlq, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-dlqDeliveries:
	case <-time.After(5 * time.Second):
		t.Fatal("malformed task was not routed to DLQ")
	}
}
