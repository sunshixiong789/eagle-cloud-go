package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	platformdb "github.com/eagle-go/eagle/app/order/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
)

const (
	outboxBatchSize          = 100
	outboxMaxAttempts        = int32(20)
	outboxPublishedRetention = 7 * 24 * time.Hour
)

var (
	outboxMeter           = otel.Meter("github.com/eagle-go/eagle/app/order/outbox")
	outboxPublished       = mustCounter("eagle_outbox_published_total", "Successfully published outbox events")
	outboxPublishFailures = mustCounter("eagle_outbox_publish_failures_total", "Failed outbox publish attempts")
	outboxParked          = mustCounter("eagle_outbox_parked_total", "Outbox events parked after retry exhaustion")
	outboxCleaned         = mustCounter("eagle_outbox_cleaned_total", "Published outbox events removed by retention")
	outboxPendingGauge    = mustGauge("eagle_outbox_pending", "Outbox events waiting to be published")
	outboxFailedGauge     = mustGauge("eagle_outbox_failed", "Outbox events parked for operator action")
	outboxOldestGauge     = mustGauge("eagle_outbox_oldest_pending_seconds", "Age of the oldest pending outbox event")
)

type outboxRecord struct {
	id, eventType, routingKey string
	payload                   []byte
	createdAt                 time.Time
	attempts                  int32
}

func NewOutboxRelay(db *platformdb.Database, publisher *rabbitmq.Publisher, logger *slog.Logger) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runOutboxRelay(ctx, db.SQL(), publisher, logger)
	}()
	return func() { cancel(); <-done }
}

func runOutboxRelay(ctx context.Context, db *sql.DB, publisher *rabbitmq.Publisher, logger *slog.Logger) {
	poll := time.NewTicker(500 * time.Millisecond)
	observe := time.NewTicker(time.Minute)
	cleanup := time.NewTicker(time.Hour)
	defer poll.Stop()
	defer observe.Stop()
	defer cleanup.Stop()

	observeOutbox(ctx, db, logger)
	for {
		publishOutboxBatch(ctx, db, publisher, logger)
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		case <-observe.C:
			observeOutbox(ctx, db, logger)
		case <-cleanup.C:
			cleanupPublishedOutbox(ctx, db, logger)
			observeOutbox(ctx, db, logger)
		}
	}
}

func publishOutboxBatch(ctx context.Context, db *sql.DB, publisher *rabbitmq.Publisher, logger *slog.Logger) {
	for range outboxBatchSize {
		record, err := claimOutbox(ctx, db)
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		if err != nil {
			if ctx.Err() == nil {
				logger.ErrorContext(ctx, "claim order outbox failed", "error", err)
			}
			return
		}
		err = publisher.Publish(ctx, rabbitmq.Message{
			ID: record.id, Type: record.eventType, RoutingKey: record.routingKey,
			Body: record.payload, Timestamp: record.createdAt,
		})
		if err != nil {
			outboxPublishFailures.Add(ctx, 1)
			parked, releaseErr := releaseOutbox(ctx, db, record.id, record.attempts, err.Error())
			if releaseErr != nil {
				logger.ErrorContext(ctx, "release order outbox failed", "event_id", record.id, "error", releaseErr)
			}
			if parked {
				outboxParked.Add(ctx, 1)
				logger.ErrorContext(ctx, "order event parked after retry exhaustion", "event_id", record.id, "attempts", record.attempts)
			} else {
				logger.WarnContext(ctx, "publish order event failed", "event_id", record.id, "attempts", record.attempts, "error", err)
			}
			return
		}
		if _, err := db.ExecContext(ctx, `UPDATE event_outbox SET published_at = now(), locked_until = NULL, last_error = '' WHERE id = $1`, record.id); err != nil {
			logger.ErrorContext(ctx, "mark order event published failed", "event_id", record.id, "error", err)
			return
		}
		outboxPublished.Add(ctx, 1)
	}
}

func claimOutbox(ctx context.Context, db *sql.DB) (*outboxRecord, error) {
	const query = `
WITH candidate AS (
    SELECT id FROM event_outbox
    WHERE published_at IS NULL AND failed_at IS NULL AND available_at <= now()
      AND (locked_until IS NULL OR locked_until < now())
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE event_outbox AS e
SET locked_until = now() + interval '30 seconds', attempts = attempts + 1
FROM candidate
WHERE e.id = candidate.id
RETURNING e.id, e.event_type, e.routing_key, e.payload, e.created_at, e.attempts`
	var record outboxRecord
	err := db.QueryRowContext(ctx, query).Scan(
		&record.id, &record.eventType, &record.routingKey, &record.payload, &record.createdAt, &record.attempts,
	)
	return &record, err
}

func releaseOutbox(ctx context.Context, db *sql.DB, id string, attempts int32, message string) (bool, error) {
	if len(message) > 1024 {
		message = message[:1024]
	}
	if attempts >= outboxMaxAttempts {
		_, err := db.ExecContext(ctx, `
UPDATE event_outbox
SET locked_until = NULL, failed_at = now(), last_error = $2
WHERE id = $1`, id, message)
		return true, err
	}
	_, err := db.ExecContext(ctx, `
UPDATE event_outbox
SET locked_until = NULL, available_at = now() + ($2 * interval '1 second'), last_error = $3
WHERE id = $1`, id, int64(retryDelay(attempts)/time.Second), message)
	return false, err
}

func retryDelay(attempts int32) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts >= 7 {
		return time.Minute
	}
	shift := attempts - 1
	return time.Second << shift
}

func cleanupPublishedOutbox(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	result, err := db.ExecContext(ctx, `
DELETE FROM event_outbox
WHERE published_at < now() - ($1 * interval '1 second')`, int64(outboxPublishedRetention/time.Second))
	if err != nil {
		if ctx.Err() == nil {
			logger.WarnContext(ctx, "cleanup published order outbox failed", "error", err)
		}
		return
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		outboxCleaned.Add(ctx, count)
		logger.InfoContext(ctx, "cleaned published order outbox", "count", count)
	}
}

func observeOutbox(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	var pending, failed int64
	var oldest float64
	err := db.QueryRowContext(ctx, `
SELECT
    count(*) FILTER (WHERE published_at IS NULL AND failed_at IS NULL),
    count(*) FILTER (WHERE failed_at IS NOT NULL),
    COALESCE(EXTRACT(EPOCH FROM now() - min(created_at) FILTER (
        WHERE published_at IS NULL AND failed_at IS NULL
    )), 0)
FROM event_outbox`).Scan(&pending, &failed, &oldest)
	if err != nil {
		if ctx.Err() == nil {
			logger.WarnContext(ctx, "observe order outbox failed", "error", err)
		}
		return
	}
	outboxPendingGauge.Record(ctx, pending)
	outboxFailedGauge.Record(ctx, failed)
	outboxOldestGauge.Record(ctx, int64(oldest))
}

func mustCounter(name, description string) metric.Int64Counter {
	instrument, err := outboxMeter.Int64Counter(name, metric.WithDescription(description))
	if err != nil {
		panic(fmt.Sprintf("create outbox counter %s: %v", name, err))
	}
	return instrument
}

func mustGauge(name, description string) metric.Int64Gauge {
	instrument, err := outboxMeter.Int64Gauge(name, metric.WithDescription(description))
	if err != nil {
		panic(fmt.Sprintf("create outbox gauge %s: %v", name, err))
	}
	return instrument
}
