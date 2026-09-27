# API、鉴权与身份

仅在修改 Proto、RPC、权限码、当前主体或授权链路时读取。

- 契约顺序：修改 Proto（含 HTTP、校验和 `access`）→ `make api` → 实现模块用例 → 注册新增 Service → 测试。
- `ACCESS_LEVEL_PERMISSION_REQUIRED` 必须声明 `perm`，其他 access 不声明。具体权限码使用 `domain:resource:action` 三段格式；通配只允许最后一段。
- 权限匹配沿用项目的末段通配语义，禁止改用 Casbin `keyMatch2`；它会把冒号后的权限段误当作 URL 参数。
- 权限码先进入 `permission_definition`，导航节点只能引用已有且启用的权限码，不能自行创造权限契约。
- handler 不根据角色或权限写 `if`。认证、内部服务身份和权限判定统一由 server 中间件完成。
- 当前主体使用 `pkg/identity.FromContext` / `Subject`；不自行解析 JWT，不新增用户表，也不把请求参数当作当前用户角色。
- 跨服务内部 RPC 使用 Client Credentials 和 `ACCESS_LEVEL_INTERNAL`；业务服务不直连 admin 权限表，远程判定失败时关闭权限接口。
- Casbin 存储适配器保持只读；策略写入沿用 access 模块现有的版本检查、审计和事务路径，不直接调用 AutoSave / `SavePolicy` / `AddPolicy`。
- domain/application 返回标准库错误；传输错误映射在 service/server 边界完成。字段校验优先写在 Proto，不在 service 重复。
- 破坏性契约变更必须通过 `buf breaking`；不要复用已有字段号表达新语义，也不要手改生成的 `*.pb.go`。
