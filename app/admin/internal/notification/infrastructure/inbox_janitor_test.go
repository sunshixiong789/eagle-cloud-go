package infrastructure

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestMaintainInboxDeletesOnlyExpiredRows(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	_, err := notificationTestDB.SQL().ExecContext(ctx, `
INSERT INTO event_inbox (event_id, event_type, processed_at) VALUES
    ('inbox-expired-test', 'test.Event', now() - ($1 * interval '1 second')),
    ('inbox-current-test', 'test.Event', now())
ON CONFLICT (event_id) DO UPDATE SET processed_at = EXCLUDED.processed_at`, int64((inboxRetention+time.Hour)/time.Second))
	if err != nil {
		t.Fatalf("insert inbox: %v", err)
	}
	defer func() {
		_, _ = notificationTestDB.SQL().ExecContext(ctx, `DELETE FROM event_inbox WHERE event_id LIKE 'inbox-%-test'`)
	}()
	maintainInbox(ctx, notificationTestDB, slog.Default())
	var expired, current int
	if err := notificationTestDB.SQL().QueryRowContext(ctx, `
SELECT
    count(*) FILTER (WHERE event_id = 'inbox-expired-test'),
    count(*) FILTER (WHERE event_id = 'inbox-current-test')
FROM event_inbox`).Scan(&expired, &current); err != nil {
		t.Fatalf("query inbox: %v", err)
	}
	if expired != 0 || current != 1 {
		t.Fatalf("inbox rows expired/current = %d/%d, want 0/1", expired, current)
	}
}
