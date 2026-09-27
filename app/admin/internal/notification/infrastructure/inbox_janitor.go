package infrastructure

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
)

const inboxRetention = 30 * 24 * time.Hour

var (
	inboxMeter   = otel.Meter("github.com/eagle-go/eagle/app/admin/notification/inbox")
	inboxEntries = mustInboxGauge("eagle_inbox_entries", "Processed event IDs retained for idempotency")
	inboxCleaned = mustInboxCounter("eagle_inbox_cleaned_total", "Expired inbox rows removed by retention")
)

func NewInboxJanitor(db *platformdb.Database, logger *slog.Logger) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		maintainInbox(ctx, db, logger)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				maintainInbox(ctx, db, logger)
			}
		}
	}()
	return func() { cancel(); <-done }
}

func maintainInbox(ctx context.Context, db *platformdb.Database, logger *slog.Logger) {
	result, err := db.SQL().ExecContext(ctx, `
DELETE FROM event_inbox
WHERE processed_at < now() - ($1 * interval '1 second')`, int64(inboxRetention/time.Second))
	if err != nil {
		if ctx.Err() == nil {
			logger.WarnContext(ctx, "cleanup notification inbox failed", "error", err)
		}
		return
	}
	if count, rowsErr := result.RowsAffected(); rowsErr == nil && count > 0 {
		inboxCleaned.Add(ctx, count)
		logger.InfoContext(ctx, "cleaned notification inbox", "count", count)
	}
	var count int64
	if err := db.SQL().QueryRowContext(ctx, `SELECT count(*) FROM event_inbox`).Scan(&count); err != nil {
		if ctx.Err() == nil {
			logger.WarnContext(ctx, "observe notification inbox failed", "error", err)
		}
		return
	}
	inboxEntries.Record(ctx, count)
}

func mustInboxCounter(name, description string) metric.Int64Counter {
	instrument, err := inboxMeter.Int64Counter(name, metric.WithDescription(description))
	if err != nil {
		panic(fmt.Sprintf("create inbox counter %s: %v", name, err))
	}
	return instrument
}

func mustInboxGauge(name, description string) metric.Int64Gauge {
	instrument, err := inboxMeter.Int64Gauge(name, metric.WithDescription(description))
	if err != nil {
		panic(fmt.Sprintf("create inbox gauge %s: %v", name, err))
	}
	return instrument
}
