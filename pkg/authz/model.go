package authz

// ModelText 是 Casbin 的 RBAC 模型定义。
//
// 请求与策略都是 (角色, 权限码) 二元组：
//
//	r = sub(角色), obj(权限码)
//	p = sub(角色), obj(权限码)
//
// 为什么 sub 是角色而不是用户：用户和角色的归属关系由 Keycloak 维护，
// 随 token 的 realm_access.roles 下发。若把用户写进 Casbin 的 g 规则，
// 就等于在本库里再维护一份用户→角色映射，与 Keycloak 双写、必然漂移。
// 中间件改为遍历 token 里的角色逐个判定，本库只存「角色→权限码」。
//
// 保留 g 是为了角色继承（例如 admin 继承 operator 的全部权限），
// 这层继承关系属于本系统的授权语义，放在这里比放 Keycloak 更合适。
//
// 通配用 keyMatch 而不是 keyMatch2。
//
// keyMatch2 是为 URL 路径设计的，会把 `:xxx` 解析成路径参数占位符。
// 我们的权限码正是冒号分隔的，于是 `system:permission:query` 会被当成
// 模式 `system:{任意}:{任意}`，与 `system:permission:add` 匹配成功——
// 结果是任何同形状的权限码互相等价，只读角色直接获得写权限。
//
// keyMatch 只认 `*`：`system:*` 覆盖整个 system 域，
// 不含 `*` 的策略退化为精确比较，符合权限码的语义。
const ModelText = `
[request_definition]
r = sub, obj

[policy_definition]
p = sub, obj

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (r.obj == p.obj || keyMatch(r.obj, p.obj))
`
