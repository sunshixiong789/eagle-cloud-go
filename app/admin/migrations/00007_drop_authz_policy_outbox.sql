-- +goose Up
-- +goose StatementBegin

-- 策略同步已改为数据库 version + 周期对账，不再使用 outbox。
DROP TABLE IF EXISTS authz_policy_outbox;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE authz_policy_outbox (
    id             bigserial   PRIMARY KEY,
    policy_version bigint      NOT NULL UNIQUE CHECK (policy_version >= 0),
    event_type     varchar(64) NOT NULL,
    payload        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),
    published_at   timestamptz,
    attempts       int         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error     text        NOT NULL DEFAULT ''
);
CREATE INDEX idx_authz_policy_outbox_pending ON authz_policy_outbox (published_at, id);

-- +goose StatementEnd
