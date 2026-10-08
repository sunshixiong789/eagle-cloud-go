# Docker Compose + nginx 生产部署

适合小团队、少量服务，以及固定的一台或两台服务器。应用仍是 admin、product、order 三个
独立发布单元，业务和数据边界不变。需要跨节点调度、自动扩缩容或 Kubernetes 平台能力时，
选用 [K3s + Envoy Gateway](../deploy/kubernetes/k3s/README.md)。两种模式都不需要额外注册中心。

生产入口是 `deploy/compose/compose.yml`，独立于开发用的 `deploy/docker-compose.yml`；
不要用两者叠加生成生产环境。镜像构建和数据库变更策略见[生产部署总览](deployment.md)。

## 拓扑与依赖

```text
客户端 → api.example.com:443 → nginx → admin / product / order
                                      product → dns:///admin:9000
                                      order   → dns:///product:9000
                                               │
                                 PostgreSQL / Redis / RabbitMQ / OSS / OIDC
```

生产清单只包含应用、nginx 和 migration profile 中的三个一次性迁移任务。数据库、Redis、
RabbitMQ、Keycloak 与 OSS 使用已有生产实例或独立维护的服务；可以采用托管实例或团队维护的
实例，但必须明确备份、恢复和故障责任。开发 realm 和开发依赖不随应用发布。

nginx 只发布 80/443。应用的 HTTP、gRPC、metrics 端口仅在该主机的 Docker 网络可达。
迁移容器只接收当前服务的迁移 DSN，应用使用自己的运行 DSN，不接收其他服务的数据库或 OSS 密钥。
应用以镜像内的 nonroot 用户运行，使用只读根文件系统、资源限制、日志轮转和优雅停止。

## 准备配置

需要 Docker Engine、Docker Compose v2.20+ 和现有生产依赖。在仓库根目录执行：

```bash
umask 077
cp deploy/compose/.env.example .env.production
chmod 600 .env.production
```

填入三个服务的镜像 digest、真实 API 域名、前端 origin、OIDC、三个 database、Redis、RabbitMQ
和 OSS 配置。`CHANGE_ME`、`example.com` 和全零 digest 只能用于静态渲染。
`.env.production` 已被 Git 忽略；不要把 `docker compose config` 的完整输出写入日志或提交。

将域名证书和私钥安装到 `EAGLE_TLS_DIR` 指定的绝对目录，文件名分别为 `fullchain.pem` 和
`privkey.pem`。目录不存在时启动失败，不会自动创建空目录。在 Linux 服务器上将私钥设置为
`root:root`、`0600`，以便已收紧 capabilities 的 nginx master 读取；发布管理员通过受控 sudo 更新。
续期后执行：

```bash
docker compose --env-file .env.production -f deploy/compose/compose.yml exec -T gateway nginx -t
docker compose --env-file .env.production -f deploy/compose/compose.yml exec -T gateway nginx -s reload
```

配置检查通过后才执行 reload。证书目录整体挂载，新文件通过原子替换更新即可。
Keycloak 配置 `eagle-admin`、`eagle-product`、`eagle-order` audience 和对应角色。
product/order 使用不同的 Client Credentials，默认名称是 `eagle-product-worker`、
`eagle-order-worker`；内部身份白名单与它们联动。不能复用开发 realm 的共享 worker secret。

数据库运行角色只拥有当前 database 的业务读写权限；迁移角色拥有 DDL 权限，并为运行角色设置
已有对象授权和新表、序列的 default privileges。Redis 使用 TLS；PostgreSQL 示例使用 `verify-full`，
RabbitMQ 使用 `amqps`。私有 CA 需要在环境清单中增加只读挂载及对应客户端 CA 配置。

生产文件存储使用 OSS。尚未创建 Bucket 时，先用开发清单的 `EAGLE_FILE_PROVIDER=local make up`
开发，不把本机文件卷用于双机生产文件共享。STS 凭据更新后重建 admin，让进程读取新凭据。
域名仅填写 hostname，origin 填写完整的 HTTPS origin，不填 nginx 语句或带路径的 URL。

```bash
make compose-prod-check
```

该命令检查必填变量和 Compose 结构，不输出凭据，也不连接外部依赖。它不证明凭据有效、证书存在、
镜像可信或依赖可用。其他受控文件可用 `COMPOSE_PROD_ENV=/absolute/path/production.env` 指定，
文件路径不要含空格。

## 首次部署与单服务更新

```bash
make compose-prod-up
make compose-prod-status
curl --fail https://api.example.com/edgez
```

首次部署拉取镜像，按 admin→product→order 串行执行迁移，再启动应用和网关并等待健康。
任一迁移失败都会阻止启动步骤。镜像内的 `/app/healthcheck` 检查应用 `/readyz`；nginx 在三个
应用健康后启动。普通容器启动或重启不会执行 migration profile。

只更新一个服务时，修改 `.env.production` 中该服务的 digest，再执行：

```bash
make compose-prod-deploy SERVICE=product
```

它只拉取 product 的新镜像，用同一镜像运行 product 迁移，再重建 product 并等待健康；
admin/order 不会被重建。Compose 没有 Kubernetes 的滚动替换保证，单实例重建可能短暂返回 502。
需要降低发布中断时使用下文的双机发布流程或 K3s 模式。

应用回滚：将该服务 digest 改回上一个版本，执行
`docker compose --env-file .env.production -f deploy/compose/compose.yml up -d --no-deps --wait product`。
回滚不执行 `goose down`，已有 schema 必须向后兼容。不要使用会重跑旧版迁移的发布目标回滚应用。

## 服务发现与网关

同一 Compose 网络使用 Docker DNS。内部 gRPC 使用 `dns:///admin:9000` 与
`dns:///product:9000`；nginx 使用 `127.0.0.11`、共享 upstream zone 和 `server ... resolve`，
每 5 秒刷新解析缓存，容器重建后无需手工 reload。DNS 更新和断线重连存在恢复窗口，
不提供业务健康检查。[Docker 网络说明](https://docs.docker.com/compose/how-tos/networking/)、
[nginx 动态解析](https://nginx.org/en/docs/http/ngx_http_upstream_module.html#server)

开发和生产 nginx 共用路径规则：`/v1/products`→product、`/v1/orders`→order，其余路径→admin。
原始路径和查询参数保持不变，Authorization 转发给应用；JWT、Casbin 和主体所有权由 Go 服务判定。
网关关闭自动重试，避免写请求被重放。生产提供 HTTP→HTTPS、TLS 1.2/1.3、单 origin CORS、超时，
以及每 IP 20 次/秒、burst 40 的限流示例；按容量和上游 LB 的客户端 IP 行为调整参数。
16 MiB 请求体上限允许默认 10 MiB 文件经过 Protobuf JSON 的 base64 编码。

`/edgez` 只表示 nginx 入口存活，不汇总应用或数据库 readiness。应用健康由容器 healthcheck 和
业务监控观察。`restart: unless-stopped` 处理进程退出和主机重启，仅 unhealthy 不会触发自动重启，
nginx 也不会根据 Docker health 状态剔除仍在运行的容器。处置时先查依赖，再重启或撤下节点。

日志用 `docker compose --env-file .env.production -f deploy/compose/compose.yml logs --tail 200 product` 查看。
metrics 9100 不发布到公网；Prometheus/Collector 可作为独立受控容器加入该主机的 Compose 网络。
OTLP 未配置时留空。备份恢复、MQ 处置见[运行手册](operations.md)。

## 两台服务器

每台独立运行完整应用栈，接入同一组生产依赖。域名指向带健康检查的云 L4 LB，将 80/443 转发到
两台 nginx，TLS 在 nginx 终止。普通 Compose bridge 和服务名仅在当前主机生效，不能用 A 的
`product` 名称找到 B 的容器。如果确实分散服务，使用可达私网地址、内部域名或内部 LB。

每台容量应能承担故障或发布期间的核心负载。LB 的 `/edgez` 探测只能发现入口故障；应用局部故障
由监控发现后撤下该节点或修复应用。数据依赖的高可用和恢复不能依赖应用副本。
每次只发布一台：

1. 从 LB 后端池撤下 A，等待已有请求结束。
2. 更新 A 的目标 digest，运行 `make compose-prod-deploy SERVICE=product`；迁移使用 expand 变更。
3. 确认 A 的 healthcheck、真实授权接口和核心用例通过，再将 A 加回 LB。
4. 撤下 B，更新为相同 digest，执行
   `docker compose --env-file .env.production -f deploy/compose/compose.yml pull product`，
   再执行 `docker compose --env-file .env.production -f deploy/compose/compose.yml up -d --no-deps --wait product`，
   避免重复编排迁移。
5. 验证 B 后加回 LB，观察错误率、延迟和 Outbox；contract 变更等旧版本全部退出后独立发布。

## 部署验收

```bash
make validate-deploy
make test-gateway
```

前者渲染开发/生产 Compose 与所有 Kubernetes 组合。后者在隔离 Docker 网络中使用假后端和临时证书，
验证 HTTPS、重定向、路径/查询参数、Authorization、CORS 和换 IP 后开发/生产入口自动恢复；
退出时清理容器、网络和证书，不替代真实生产依赖、身份和云 LB 的上线验证。
