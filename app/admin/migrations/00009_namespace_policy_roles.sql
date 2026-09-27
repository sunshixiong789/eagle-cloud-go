-- +goose Up
-- +goose StatementBegin

-- 旧版本把 realm role 与 client role 都按裸名称存储，无法区分来源。
-- 历史裸角色只能按 realm role 迁移；新写入的 client:* 键保持不变。
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT ptype, 'realm:' || v0, v1, v2, v3, v4, v5
FROM casbin_rule
WHERE ptype = 'p'
  AND v0 NOT LIKE 'realm:%'
  AND v0 NOT LIKE 'client:%'
ON CONFLICT DO NOTHING;

DELETE FROM casbin_rule
WHERE ptype = 'p'
  AND v0 NOT LIKE 'realm:%'
  AND v0 NOT LIKE 'client:%';

INSERT INTO casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT
    ptype,
    CASE WHEN v0 LIKE 'realm:%' OR v0 LIKE 'client:%' THEN v0 ELSE 'realm:' || v0 END,
    CASE WHEN v1 LIKE 'realm:%' OR v1 LIKE 'client:%' THEN v1 ELSE 'realm:' || v1 END,
    v2, v3, v4, v5
FROM casbin_rule
WHERE ptype = 'g'
  AND (
    (v0 NOT LIKE 'realm:%' AND v0 NOT LIKE 'client:%')
    OR (v1 NOT LIKE 'realm:%' AND v1 NOT LIKE 'client:%')
  )
ON CONFLICT DO NOTHING;

DELETE FROM casbin_rule
WHERE ptype = 'g'
  AND (
    (v0 NOT LIKE 'realm:%' AND v0 NOT LIKE 'client:%')
    OR (v1 NOT LIKE 'realm:%' AND v1 NOT LIKE 'client:%')
  );

UPDATE authz_policy_state
SET version = version + 1, updated_at = now()
WHERE id = 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

INSERT INTO casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT ptype, substring(v0 FROM 7), v1, v2, v3, v4, v5
FROM casbin_rule
WHERE ptype = 'p' AND v0 LIKE 'realm:%'
ON CONFLICT DO NOTHING;

DELETE FROM casbin_rule
WHERE ptype = 'p' AND v0 LIKE 'realm:%';

INSERT INTO casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT
    ptype,
    CASE WHEN v0 LIKE 'realm:%' THEN substring(v0 FROM 7) ELSE v0 END,
    CASE WHEN v1 LIKE 'realm:%' THEN substring(v1 FROM 7) ELSE v1 END,
    v2, v3, v4, v5
FROM casbin_rule
WHERE ptype = 'g' AND (v0 LIKE 'realm:%' OR v1 LIKE 'realm:%')
ON CONFLICT DO NOTHING;

DELETE FROM casbin_rule
WHERE ptype = 'g' AND (v0 LIKE 'realm:%' OR v1 LIKE 'realm:%');

UPDATE authz_policy_state
SET version = version + 1, updated_at = now()
WHERE id = 1;

-- +goose StatementEnd
