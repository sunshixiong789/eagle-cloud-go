-- +goose Up
-- +goose StatementBegin

CREATE TABLE permission_tree_state (
    id         bigint      PRIMARY KEY CHECK (id = 1),
    revision   bigint      NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO permission_tree_state (id, revision) VALUES (1, 1);

ALTER TABLE sys_permission ALTER COLUMN parent_id DROP DEFAULT;
ALTER TABLE sys_permission ALTER COLUMN parent_id DROP NOT NULL;
UPDATE sys_permission SET parent_id = NULL WHERE parent_id = 0;
ALTER TABLE sys_permission
    ADD CONSTRAINT fk_sys_permission_parent
    FOREIGN KEY (parent_id) REFERENCES sys_permission(id) ON DELETE RESTRICT;
ALTER TABLE sys_permission
    ADD CONSTRAINT ck_sys_permission_not_self_parent
    CHECK (parent_id IS NULL OR parent_id <> id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sys_permission DROP CONSTRAINT IF EXISTS ck_sys_permission_not_self_parent;
ALTER TABLE sys_permission DROP CONSTRAINT IF EXISTS fk_sys_permission_parent;
UPDATE sys_permission SET parent_id = 0 WHERE parent_id IS NULL;
ALTER TABLE sys_permission ALTER COLUMN parent_id SET NOT NULL;
ALTER TABLE sys_permission ALTER COLUMN parent_id SET DEFAULT 0;
DROP TABLE IF EXISTS permission_tree_state;
-- +goose StatementEnd
