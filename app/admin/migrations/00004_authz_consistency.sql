-- +goose Up
-- +goose StatementBegin

CREATE TABLE authz_policy_state (
    id         bigint      PRIMARY KEY CHECK (id = 1),
    version    bigint      NOT NULL DEFAULT 0 CHECK (version >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO authz_policy_state (id, version) VALUES (1, 1);

CREATE TABLE authz_policy_audit (
    id              bigserial    PRIMARY KEY,
    policy_version  bigint       NOT NULL CHECK (policy_version >= 0),
    action          varchar(64)  NOT NULL,
    target          varchar(256) NOT NULL,
    actor_subject   varchar(128) NOT NULL DEFAULT '',
    actor_client_id varchar(128) NOT NULL DEFAULT '',
    request_id      varchar(128) NOT NULL DEFAULT '',
    trace_id        varchar(64)  NOT NULL DEFAULT '',
    before          jsonb        NOT NULL DEFAULT '[]'::jsonb,
    after           jsonb        NOT NULL DEFAULT '[]'::jsonb,
    created_at      timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_authz_policy_audit_version ON authz_policy_audit (policy_version);
CREATE INDEX idx_authz_policy_audit_target_created ON authz_policy_audit (target, created_at);
CREATE INDEX idx_authz_policy_audit_actor_created ON authz_policy_audit (actor_subject, created_at);

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

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS authz_policy_outbox;
DROP TABLE IF EXISTS authz_policy_audit;
DROP TABLE IF EXISTS authz_policy_state;
-- +goose StatementEnd
