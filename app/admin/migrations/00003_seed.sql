-- +goose Up
-- +goose StatementBegin

-- 只种入不含任何机密的基础数据。
-- 用户和角色都在 Keycloak，这里不再创建任何账号——
-- 内置已知口令的账号会一路带到生产。

-- ── 权限树 ────────────────────────────────────────────
-- type: 1=目录 2=菜单 3=按钮
INSERT INTO sys_permission (id, parent_id, name, code, type, path, component, icon, sort) VALUES
    (1,   0,   '系统管理', '',                        1, '/system',    '',                  'settings', 1),

    (100, 1,   '用户管理', 'system:user:list',        2, 'user',       'system/user/index', 'user',     1),
    (101, 100, '用户查询', 'system:user:query',       3, '', '', '', 1),
    (102, 100, '用户编辑', 'system:user:edit',        3, '', '', '', 2),

    (110, 1,   '角色权限', 'system:role:list',        2, 'role',       'system/role/index', 'peoples',  2),
    (111, 110, '角色查询', 'system:role:query',       3, '', '', '', 1),
    (112, 110, '权限分配', 'system:role:assign',      3, '', '', '', 2),

    (120, 1,   '权限管理', 'system:permission:list',  2, 'permission', 'system/perm/index', 'tree',     3),
    (121, 120, '权限查询', 'system:permission:query', 3, '', '', '', 1),
    (122, 120, '权限新增', 'system:permission:add',   3, '', '', '', 2),
    (123, 120, '权限修改', 'system:permission:edit',  3, '', '', '', 3),
    (124, 120, '权限删除', 'system:permission:remove',3, '', '', '', 4),

    (130, 1,   '字典管理', 'system:dict:list',        2, 'dict',       'system/dict/index', 'dict',     4),
    (131, 130, '字典查询', 'system:dict:query',       3, '', '', '', 1),
    (132, 130, '字典新增', 'system:dict:add',         3, '', '', '', 2),
    (133, 130, '字典修改', 'system:dict:edit',        3, '', '', '', 3),
    (134, 130, '字典删除', 'system:dict:remove',      3, '', '', '', 4);

-- 显式指定过 id，把序列推到最大值之后，否则后续 INSERT 会主键冲突
SELECT setval(pg_get_serial_sequence('sys_permission', 'id'), (SELECT max(id) FROM sys_permission));

-- ── Casbin 策略 ───────────────────────────────────────
-- 策略主体显式区分 realm 与 client role，避免同名角色碰撞。
-- realm:admin 走正常 Casbin 判定；超管短路只认本服务 client role。
INSERT INTO casbin_rule (ptype, v0, v1) VALUES
    -- 通配：realm admin 拥有 system 域下全部权限
    ('p', 'realm:admin', 'system:*'),
    -- 普通用户只读
    ('p', 'realm:user', 'system:user:list'),
    ('p', 'realm:user', 'system:user:query'),
    ('p', 'realm:user', 'system:role:list'),
    ('p', 'realm:user', 'system:role:query'),
    ('p', 'realm:user', 'system:permission:list'),
    ('p', 'realm:user', 'system:permission:query'),
    ('p', 'realm:user', 'system:dict:list'),
    ('p', 'realm:user', 'system:dict:query');

-- 角色继承：realm admin 自动获得 realm user 的全部权限
INSERT INTO casbin_rule (ptype, v0, v1) VALUES ('g', 'realm:admin', 'realm:user');

-- ── 字典 ──────────────────────────────────────────────
INSERT INTO sys_dict_type (name, type, remark) VALUES
    ('通用状态', 'sys_common_status',   '启用/禁用'),
    ('权限类型', 'sys_permission_type', '目录/菜单/按钮'),
    ('是否',     'sys_yes_no',          '通用是否标识');

INSERT INTO sys_dict_data (dict_type, label, value, sort, css_class, is_default) VALUES
    ('sys_common_status',   '正常', '1', 1, 'success', true),
    ('sys_common_status',   '禁用', '0', 2, 'danger',  false),

    ('sys_permission_type', '目录', '1', 1, '', true),
    ('sys_permission_type', '菜单', '2', 2, '', false),
    ('sys_permission_type', '按钮', '3', 3, '', false),

    ('sys_yes_no',          '是',   'Y', 1, '', false),
    ('sys_yes_no',          '否',   'N', 2, '', true);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM sys_dict_data WHERE dict_type IN ('sys_common_status','sys_permission_type','sys_yes_no');
DELETE FROM sys_dict_type WHERE type IN ('sys_common_status','sys_permission_type','sys_yes_no');
DELETE FROM casbin_rule WHERE v0 IN ('realm:admin','realm:user');
DELETE FROM sys_permission WHERE id <= 200;
-- +goose StatementEnd
