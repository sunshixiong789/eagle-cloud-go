# Keycloak realm 配置

`realm-eagle.json` 在 Keycloak 启动时经 `--import-realm` 自动导入。

## 为什么这个文件里没有注释

JSON 没有注释语法，而 Keycloak 的 `RealmRepresentation` 用 Jackson 的严格模式
反序列化——**任何未知字段都会让整次导入失败**，容器直接以 exit 1 退出：

```
ERROR: Failed to run import
ERROR: Unrecognized field "_comment_ttl" (class RealmRepresentation), not marked as ignorable
```

用 `_comment` 这类键做行内注释在别处常见，在这里行不通。所以说明都写在本文件里。

## 配置说明

### Token 生命周期

`accessTokenLifespan` 设为 900 秒（15 分钟）。资源服务器本地验签，不维护
token 黑名单；会话撤销由 Keycloak 管理，已签发 access token 最迟在
15 分钟后自然失效，因此保持短寿命。

### refresh token 必须一次性使用

```json
"revokeRefreshToken": true,
"refreshTokenMaxReuse": 0
```

**这两项默认是关的，关着就是一个真实的安全缺口。**

默认行为下 Keycloak 每次续期都会下发新的 refresh token，但**旧的仍然有效**。
于是 refresh token 一旦从浏览器存储、日志或代理中泄漏，攻击者可以在整个
`refresh_expires_in` 窗口内反复换取新的 access token，而合法用户毫无察觉。

`eagle-web` 是公共客户端（SPA，无法保存 secret），OAuth 2.0 Security BCP
对这类客户端明确要求 refresh token 一次性使用并具备重放检测能力。开启后，
一个已被用过的 refresh token 再次出现会被判定为重放，Keycloak 直接作废
整个会话——这正是发现令牌被盗的手段。

代价：客户端必须每次都保存续期返回的新 refresh token。用旧的会被拒，
这是预期行为而非故障。

### 客户端

| clientId | 用途 | 关键配置 |
|---|---|---|
| `eagle-web` | 管理后台前端（SPA） | 公共客户端，强制 PKCE（S256）。SPA 无法安全保存 secret，因此不发 secret |
| `eagle-admin/product/order` | 三个资源服务器 | 各自 `bearerOnly`，audience 和 client role 命名空间相互隔离 |
| `eagle-system` | 兼容旧本地配置 | 新部署使用上面三个 client |
| `eagle-worker` | 服务间调用 | 机密客户端，`client_credentials` 授权；token 含 admin/product/order audience |

### audience mapper 是必需的

Keycloak 默认把 access token 的 `aud` 设成 `account`，对资源服务器毫无意义。
`eagle-web` 配置了 audience mapper，把三个资源服务都写进 `aud`；
Compose 为每个服务配置对应 audience，因此 token 不能被错误的资源服务接受。

不配这个 mapper 的话，要么校验永远失败，要么只能关掉 `aud` 校验——
后者等于放弃了「这个 token 是发给我的」这层保证。

### 关于 `eagle-worker` 的 secret

`dev-only-worker-secret` **仅供本地开发**。生产环境必须：

1. 从本文件移除 `secret` 字段，让 Keycloak 自动生成
2. 经环境变量或 K8s Secret 注入调用方
3. 定期轮转

留在这里是为了让 `docker compose up` 后无需额外步骤就能跑通端到端测试。
它只在这个一次性的本地 realm 里有效。

### 为什么不预置用户

本文件会进版本库。内置已知口令的账号会一路带到生产环境，这类账号往往
在上线很久后才被发现。

首个管理员请自行创建：

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh config credentials --server http://localhost:8080 --realm master --user admin --password admin
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh create users -r eagle \
  -s username=alice -s enabled=true \
  -s firstName=Alice -s lastName=Admin -s email=alice@example.com
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh set-password -r eagle --username alice --new-password 'ChangeMe!'
```

```bash
docker exec eagle-keycloak /opt/keycloak/bin/kcadm.sh add-roles -r eagle --uusername alice --rolename admin
```

### `sslRequired: none`

仅因本地开发走明文 HTTP。**生产环境务必改回 `external` 或 `all`**，
否则 token 会在网络上明文传输。
