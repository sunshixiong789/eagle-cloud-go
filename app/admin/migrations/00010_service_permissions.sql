-- +goose Up
-- +goose StatementBegin
INSERT INTO permission_definition (code, service, resource, action, status, source) VALUES
    ('system:file:add', 'system', 'file', 'add', 1, 'migration'),
    ('system:file:remove', 'system', 'file', 'remove', 1, 'migration'),
    ('system:notification:send', 'system', 'notification', 'send', 1, 'migration'),
    ('product:product:add', 'product', 'product', 'add', 1, 'migration'),
    ('product:product:edit', 'product', 'product', 'edit', 1, 'migration'),
    ('product:product:remove', 'product', 'product', 'remove', 1, 'migration')
ON CONFLICT (code) DO NOTHING;

INSERT INTO casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
VALUES ('p', 'realm:admin', 'product:*', '', '', '', '')
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM permission_definition WHERE code IN (
    'system:file:add', 'system:file:remove', 'system:notification:send',
    'product:product:add', 'product:product:edit', 'product:product:remove'
);
DELETE FROM casbin_rule WHERE ptype = 'p' AND v0 = 'realm:admin' AND v1 = 'product:*';
-- +goose StatementEnd
