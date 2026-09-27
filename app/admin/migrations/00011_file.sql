-- +goose Up
-- +goose StatementBegin
CREATE TABLE stored_file (
    id              varchar(36)  PRIMARY KEY,
    owner_subject   varchar(128) NOT NULL,
    name            varchar(255) NOT NULL,
    storage_key     varchar(255) NOT NULL UNIQUE,
    content_type    varchar(128) NOT NULL DEFAULT 'application/octet-stream',
    size            bigint       NOT NULL CHECK (size >= 0),
    sha256          varchar(64)  NOT NULL,
    created_at      timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_stored_file_owner_created ON stored_file (owner_subject, created_at DESC);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS stored_file;
