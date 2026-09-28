package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func brokerChannel(t *testing.T) (string, *amqp.Channel, string) {
	t.Helper()
	url := os.Getenv("EAGLE_TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("set EAGLE_TEST_RABBITMQ_URL to run real broker tests")
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	exchange := fmt.Sprintf("eagle.test.%d", time.Now().UnixNano())
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.ExchangeDelete(exchange, false, false) })
	return url, ch, exchange
}

func TestBrokerPublisherRejectsUnroutableAndReconnects(t *testing.T) {
	url, ch, exchange := brokerChannel(t)
	p, err := NewPublisher(Config{URL: url, Exchange: exchange, PublishTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	message := Message{ID: "same-event", RoutingKey: "created", Body: []byte("payload")}
	if err := p.Publish(ctx, message); !errors.Is(err, ErrUnroutable) {
		t.Fatalf("unroutable publish: %v", err)
	}
	queue, err := ch.QueueDeclare("", false, false, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(queue.Name, "created", exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(ctx, message); err != nil {
		t.Fatal(err)
	}
	// Force a transport disconnect, then retry the same event identity.
	p.gate <- struct{}{}
	p.reset()
	<-p.gate
	if err := p.Publish(ctx, message); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	for range 2 {
		delivery, ok, err := ch.Get(queue.Name, true)
		if err != nil || !ok || delivery.MessageId != message.ID {
			t.Fatalf("delivery: %v %v %v", delivery.MessageId, ok, err)
		}
	}
}

func TestConsumerShutdownRequeuesInsteadOfDeadLettering(t *testing.T) {
	url, ch, exchange := brokerChannel(t)
	queue := exchange + ".queue"
	if err := declareConsumerTopology(ch, exchange, queue, "created"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ch.QueueDelete(queue, false, false, false)
		_, _ = ch.QueueDelete(queue+".dlq", false, false, false)
		_ = ch.ExchangeDelete(exchange+".dlx", false, false)
	})
	p, err := NewPublisher(Config{URL: url, Exchange: exchange})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Publish(context.Background(), Message{ID: "shutdown", RoutingKey: "created"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunConsumer(ctx, Config{URL: url, Exchange: exchange, ConsumerAttempts: 3, RetryBackoff: time.Second}, queue, "created", slog.Default(), func(ctx context.Context, _ Message) error { close(entered); <-ctx.Done(); return ctx.Err() })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer never received message")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not stop")
	}
	// Broker channel shutdown is asynchronous; poll for the requeued delivery.
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, ok, err := ch.Get(queue, true)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown message was not requeued")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok, err := ch.Get(queue+".dlq", true); err != nil || ok {
		t.Fatalf("shutdown incorrectly dead-lettered message: %v %v", ok, err)
	}
}

func TestPublisherBoundsStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		<-done
	}()
	defer close(done)
	p, err := NewPublisher(Config{URL: "amqp://guest:guest@" + listener.Addr().String() + "/", Exchange: "test", PublishTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	start := time.Now()
	if err := p.Publish(context.Background(), Message{}); err == nil {
		t.Fatal("stalled handshake succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("handshake exceeded bounded timeout: %s", elapsed)
	}
}

func TestQuorumDeadLetterSurvivesMissingBinding(t *testing.T) {
	url, ch, exchange := brokerChannel(t)
	queue := exchange + ".quorum"
	if err := declareConsumerTopology(ch, exchange, queue, "created"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ch.QueueDelete(queue, false, false, false)
		_, _ = ch.QueueDelete(queue+".dlq", false, false, false)
		_ = ch.ExchangeDelete(exchange+".dlx", false, false)
	})
	if err := ch.QueueUnbind(queue+".dlq", "", exchange+".dlx", nil); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewPublisher(Config{URL: url, Exchange: exchange})
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	if err := publisher.Publish(context.Background(), Message{ID: "dead-letter-retained", RoutingKey: "created", Body: []byte("payload")}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		delivery, ok, err := ch.Get(queue, false)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			if err := delivery.Nack(false, false); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("message not available")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Give the internal dead-letter publisher time to discover the missing route.
	time.Sleep(500 * time.Millisecond)
	if err := ch.QueueBind(queue+".dlq", "", exchange+".dlx", false, nil); err != nil {
		t.Fatal(err)
	}
	// RabbitMQ retries periodically; current broker defaults can be three minutes.
	deadline = time.Now().Add(4 * time.Minute)
	for {
		delivery, ok, err := ch.Get(queue+".dlq", true)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			if delivery.MessageId != "dead-letter-retained" {
				t.Fatalf("unexpected message: %s", delivery.MessageId)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dead-letter delivery did not recover after restoring binding")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
