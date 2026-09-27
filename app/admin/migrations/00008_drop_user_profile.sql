-- +goose Up
-- +goose StatementBegin

-- 用户主数据在 Keycloak。部门/岗位等业务扩展还没有调用方，不预留空表。
DROP TABLE IF EXISTS sys_user_profile;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE sys_user_profile (
    id           bigserial   PRIMARY KEY,
    subject      varchar(64) NOT NULL UNIQUE,
    dept_id      bigint      NOT NULL DEFAULT 0,
    position     varchar(64) NOT NULL DEFAULT '',
    remark       text        NOT NULL DEFAULT '',
    last_seen_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_sys_user_profile_dept ON sys_user_profile (dept_id);

-- +goose StatementEnd
