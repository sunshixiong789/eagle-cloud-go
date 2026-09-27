-- +goose Up
-- +goose StatementBegin

CREATE TABLE permission_definition (
    id         bigserial    PRIMARY KEY,
    code       varchar(128) NOT NULL UNIQUE,
    service    varchar(64)  NOT NULL,
    resource   varchar(64)  NOT NULL,
    action     varchar(64)  NOT NULL,
    status     int          NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    source     varchar(32)  NOT NULL DEFAULT 'migration',
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX idx_permission_definition_parts ON permission_definition (service, resource, action);

INSERT INTO permission_definition (code, service, resource, action, status, source)
SELECT code,
       split_part(code, ':', 1),
       split_part(code, ':', 2),
       split_part(code, ':', 3),
       1,
       'migration'
FROM sys_permission
WHERE code <> '';

CREATE TABLE navigation_node (
    id              bigserial    PRIMARY KEY,
    parent_id       bigint,
    name            varchar(64)  NOT NULL,
    permission_code varchar(128) UNIQUE REFERENCES permission_definition(code) ON DELETE SET NULL,
    type            int          NOT NULL,
    path            varchar(255) NOT NULL DEFAULT '',
    component       varchar(255) NOT NULL DEFAULT '',
    icon            varchar(64)  NOT NULL DEFAULT '',
    sort            int          NOT NULL DEFAULT 0,
    visible         boolean      NOT NULL DEFAULT true,
    status          int          NOT NULL DEFAULT 1,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT fk_navigation_node_parent FOREIGN KEY (parent_id) REFERENCES navigation_node(id) ON DELETE RESTRICT,
    CONSTRAINT ck_navigation_node_not_self_parent CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX idx_navigation_node_parent ON navigation_node (parent_id);

INSERT INTO navigation_node (
    id, parent_id, name, permission_code, type, path, component, icon,
    sort, visible, status, created_at, updated_at
)
SELECT id, parent_id, name, NULLIF(code, ''), type, path, component, icon,
       sort, visible, status, created_at, updated_at
FROM sys_permission;

SELECT setval(pg_get_serial_sequence('navigation_node', 'id'), COALESCE((SELECT max(id) FROM navigation_node), 1));
SELECT setval(pg_get_serial_sequence('permission_definition', 'id'), COALESCE((SELECT max(id) FROM permission_definition), 1));
DROP TABLE sys_permission;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE sys_permission (
    id         bigserial    PRIMARY KEY,
    parent_id  bigint,
    name       varchar(64)  NOT NULL,
    code       varchar(128) NOT NULL DEFAULT '',
    type       int          NOT NULL,
    path       varchar(255) NOT NULL DEFAULT '',
    component  varchar(255) NOT NULL DEFAULT '',
    icon       varchar(64)  NOT NULL DEFAULT '',
    sort       int          NOT NULL DEFAULT 0,
    visible    boolean      NOT NULL DEFAULT true,
    status     int          NOT NULL DEFAULT 1,
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT fk_sys_permission_parent FOREIGN KEY (parent_id) REFERENCES sys_permission(id) ON DELETE RESTRICT,
    CONSTRAINT ck_sys_permission_not_self_parent CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE UNIQUE INDEX uk_sys_permission_code ON sys_permission (code) WHERE code <> '';
CREATE INDEX idx_sys_permission_parent ON sys_permission (parent_id);

INSERT INTO sys_permission (
    id, parent_id, name, code, type, path, component, icon,
    sort, visible, status, created_at, updated_at
)
SELECT id, parent_id, name, COALESCE(permission_code, ''), type, path, component, icon,
       sort, visible, status, created_at, updated_at
FROM navigation_node;
SELECT setval(pg_get_serial_sequence('sys_permission', 'id'), COALESCE((SELECT max(id) FROM sys_permission), 1));

DROP TABLE navigation_node;
DROP TABLE permission_definition;

-- +goose StatementEnd
