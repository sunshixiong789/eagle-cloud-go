-- +goose Up
-- +goose StatementBegin
CREATE TABLE event_inbox (
    event_id     varchar(36)  PRIMARY KEY,
    event_type   varchar(128) NOT NULL,
    processed_at timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_event_inbox_processed ON event_inbox (processed_at);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS event_inbox;
