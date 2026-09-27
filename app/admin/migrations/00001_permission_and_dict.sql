-- +goose Up
-- +goose StatementBegin

-- 用户扩展字段。
--
-- Keycloak 是用户的唯一来源：账号、口令、邮箱、角色全在那边。
-- 本表只存 Keycloak 不该管的业务属性，用 token 的 sub 关联。
-- 刻意不冗余用户名/邮箱——冗余就要处理与 Keycloak 的一致性同步，
-- 而那正是选择「单一用户源」时想避开的问题。
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

-- 权限节点（目录 / 菜单 / 按钮三合一的树）。
--
-- 权限码是三方契约的交汇点：proto 注解、Casbin 策略、本表，
-- 三处对不上就是全线 403。
CREATE TABLE sys_permission (
    id         bigserial    PRIMARY KEY,
    parent_id  bigint       NOT NULL DEFAULT 0,
    name       varchar(64)  NOT NULL,
    code       varchar(128) NOT NULL DEFAULT '',
    -- 1=目录 2=菜单 3=按钮
    type       int          NOT NULL,
    path       varchar(255) NOT NULL DEFAULT '',
    component  varchar(255) NOT NULL DEFAULT '',
    icon       varchar(64)  NOT NULL DEFAULT '',
    sort       int          NOT NULL DEFAULT 0,
    visible    boolean      NOT NULL DEFAULT true,
    -- 0=禁用 1=正常
    status     int          NOT NULL DEFAULT 1,
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now()
);
-- 权限码唯一，但允许多个目录/菜单留空
CREATE UNIQUE INDEX uk_sys_permission_code ON sys_permission (code) WHERE code <> '';
CREATE INDEX idx_sys_permission_parent ON sys_permission (parent_id);

-- 字典类型
CREATE TABLE sys_dict_type (
    id         bigserial   PRIMARY KEY,
    name       varchar(64) NOT NULL,
    type       varchar(64) NOT NULL UNIQUE,
    status     int         NOT NULL DEFAULT 1,
    remark     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- 字典项
CREATE TABLE sys_dict_data (
    id         bigserial    PRIMARY KEY,
    dict_type  varchar(64)  NOT NULL REFERENCES sys_dict_type (type) ON UPDATE CASCADE ON DELETE CASCADE,
    label      varchar(128) NOT NULL,
    value      varchar(128) NOT NULL,
    sort       int          NOT NULL DEFAULT 0,
    css_class  varchar(64)  NOT NULL DEFAULT '',
    is_default boolean      NOT NULL DEFAULT false,
    status     int          NOT NULL DEFAULT 1,
    remark     text         NOT NULL DEFAULT '',
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now(),
    UNIQUE (dict_type, value)
);
CREATE INDEX idx_sys_dict_data_type_sort ON sys_dict_data (dict_type, sort);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sys_dict_data;
DROP TABLE IF EXISTS sys_dict_type;
DROP TABLE IF EXISTS sys_permission;
DROP TABLE IF EXISTS sys_user_profile;
-- +goose StatementEnd
