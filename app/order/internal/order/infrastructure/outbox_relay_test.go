package infrastructure

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
)

func TestRetryDelayIsExponentiallyBounded(t *testing.T) {
	tests := []struct {
		attempts int32
		want     time.Duration
	}{{0, time.Second}, {1, time.Second}, {2, 2 * time.Second}, {7, time.Minute}, {20, time.Minute}}
	for _, test := range tests {
		if got := retryDelay(test.attempts); got != test.want {
			t.Errorf("retryDelay(%d) = %s, want %s", test.attempts, got, test.want)
		}
	}
}

func TestReleaseOutboxParksExhaustedEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	const id = "outbox-park-test"
	_, err := testDB.SQL().ExecContext(ctx, `
INSERT INTO event_outbox (id, aggregate_id, event_type, routing_key, payload, attempts)
VALUES ($1, 'order-park-test', 'test.Event', 'test.event', '\x01', $2)
ON CONFLICT (id) DO UPDATE SET failed_at = NULL, attempts = EXCLUDED.attempts`, id, outboxMaxAttempts)
	if err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	defer func() { _, _ = testDB.SQL().ExecContext(ctx, `DELETE FROM event_outbox WHERE id = $1`, id) }()
	parked, err := releaseOutbox(ctx, testDB.SQL(), id, outboxMaxAttempts, "broker unavailable")
	if err != nil || !parked {
		t.Fatalf("releaseOutbox = parked %v, err %v", parked, err)
	}
	var failed bool
	if err := testDB.SQL().QueryRowContext(ctx, `SELECT failed_at IS NOT NULL FROM event_outbox WHERE id = $1`, id).Scan(&failed); err != nil {
		t.Fatalf("query failed state: %v", err)
	}
	if !failed {
		t.Fatal("event was not parked")
	}
}

func TestOutboxKeepsUnroutableEventPending(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	url := os.Getenv("EAGLE_TEST_RABBITMQ_URL")
	if url == "" {
		t.Skip("set EAGLE_TEST_RABBITMQ_URL")
	}
	ctx := context.Background()
	id := fmt.Sprintf("outbox-route-%d", time.Now().UnixNano())
	exchange := id
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()
	defer func() { _ = ch.ExchangeDelete(exchange, false, false) }()
	publisher, err := rabbitmq.NewPublisher(rabbitmq.Config{URL: url, Exchange: exchange})
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	// Oldest timestamp makes this isolated record the relay's first candidate.
	_, err = testDB.SQL().ExecContext(ctx, `INSERT INTO event_outbox (id, aggregate_id, event_type, routing_key, payload, created_at) VALUES ($1, $1, 'test.Event', 'test.event', '\x01', '2000-01-01')`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = testDB.SQL().ExecContext(ctx, "DELETE FROM event_outbox WHERE id = $1", id) }()
	publishOutboxBatch(ctx, testDB.SQL(), publisher, slog.Default())
	var published bool
	var lastError string
	if err := testDB.SQL().QueryRowContext(ctx, "SELECT published_at IS NOT NULL, last_error FROM event_outbox WHERE id = $1", id).Scan(&published, &lastError); err != nil {
		t.Fatal(err)
	}
	if published || !strings.Contains(lastError, "not routed") {
		t.Fatalf("unroutable event incorrectly completed: %v %s", published, lastError)
	}
	queue, err := ch.QueueDeclare("", false, false, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(queue.Name, "test.event", exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.SQL().ExecContext(ctx, "UPDATE event_outbox SET available_at = now() WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	publishOutboxBatch(ctx, testDB.SQL(), publisher, slog.Default())
	if err := testDB.SQL().QueryRowContext(ctx, "SELECT published_at IS NOT NULL FROM event_outbox WHERE id = $1", id).Scan(&published); err != nil || !published {
		t.Fatalf("routed event did not complete: %v %v", published, err)
	}
	delivery, ok, err := ch.Get(queue.Name, true)
	if err != nil || !ok || delivery.MessageId != id {
		t.Fatalf("delivery: %v %v %v", delivery.MessageId, ok, err)
	}
}
