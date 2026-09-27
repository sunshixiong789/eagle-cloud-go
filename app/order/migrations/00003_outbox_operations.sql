-- +goose Up
ALTER TABLE event_outbox ADD COLUMN failed_at timestamptz;
DROP INDEX idx_event_outbox_pending;
CREATE INDEX idx_event_outbox_pending
    ON event_outbox (published_at, failed_at, available_at, created_at)
    WHERE published_at IS NULL AND failed_at IS NULL;
CREATE INDEX idx_event_outbox_failed
    ON event_outbox (failed_at, created_at)
    WHERE failed_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_event_outbox_failed;
DROP INDEX idx_event_outbox_pending;
CREATE INDEX idx_event_outbox_pending
    ON event_outbox (published_at, available_at, created_at)
    WHERE published_at IS NULL;
ALTER TABLE event_outbox DROP COLUMN failed_at;
