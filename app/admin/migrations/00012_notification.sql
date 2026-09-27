-- +goose Up
-- +goose StatementBegin
CREATE TABLE notification (
    id                bigserial     PRIMARY KEY,
    recipient_subject varchar(128)  NOT NULL,
    sender_subject    varchar(128)  NOT NULL DEFAULT '',
    title             varchar(128)  NOT NULL,
    content           varchar(4096) NOT NULL,
    read_at           timestamptz,
    created_at        timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX idx_notification_recipient_created ON notification (recipient_subject, created_at DESC);
CREATE INDEX idx_notification_recipient_unread ON notification (recipient_subject, id) WHERE read_at IS NULL;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS notification;
