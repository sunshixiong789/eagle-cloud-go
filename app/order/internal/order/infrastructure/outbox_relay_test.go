package infrastructure

import (
	"context"
	"testing"
	"time"
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
