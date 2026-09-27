-- +goose Up
-- +goose StatementBegin
CREATE TABLE event_outbox (
    id            varchar(36)   PRIMARY KEY,
    aggregate_id  varchar(128)  NOT NULL,
    event_type    varchar(128)  NOT NULL,
    routing_key   varchar(128)  NOT NULL,
    payload       bytea         NOT NULL,
    attempts      integer       NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error    varchar(1024) NOT NULL DEFAULT '',
    available_at  timestamptz   NOT NULL DEFAULT now(),
    locked_until  timestamptz,
    published_at  timestamptz,
    created_at    timestamptz   NOT NULL DEFAULT now(),
    UNIQUE (aggregate_id, event_type)
);
CREATE INDEX idx_event_outbox_pending
    ON event_outbox (published_at, available_at, created_at)
    WHERE published_at IS NULL;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS event_outbox;
