-- +goose Up
-- +goose StatementBegin

-- Casbin 策略表。
--
-- 字段命名沿用 Casbin 生态惯例（ptype + v0..v5），运维和排障时
-- 可以直接套用社区文档与既有 SQL。
--
-- 本项目实际使用：
--   p, <角色>, <权限码>    角色被授予的权限
--   g, <子角色>, <父角色>  角色继承
--
-- 角色本身由 Keycloak 维护并随 token 下发，本表只存「角色 -> 权限码」映射。
-- 不在这里维护用户到角色的关系，否则就是与 Keycloak 双写，必然漂移。
CREATE TABLE casbin_rule (
    id    bigserial   PRIMARY KEY,
    ptype varchar(8)  NOT NULL,
    v0    varchar(128) NOT NULL DEFAULT '',
    v1    varchar(128) NOT NULL DEFAULT '',
    v2    varchar(128) NOT NULL DEFAULT '',
    v3    varchar(128) NOT NULL DEFAULT '',
    v4    varchar(128) NOT NULL DEFAULT '',
    v5    varchar(128) NOT NULL DEFAULT ''
);

-- 整行唯一：重复写入同一条策略不改变判定结果，只会让表持续膨胀
CREATE UNIQUE INDEX uk_casbin_rule ON casbin_rule (ptype, v0, v1, v2, v3, v4, v5);
-- 按角色过滤是最热路径（RemoveFilteredPolicy / GetFilteredPolicy）
CREATE INDEX idx_casbin_rule_ptype_v0 ON casbin_rule (ptype, v0);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS casbin_rule;
-- +goose StatementEnd
