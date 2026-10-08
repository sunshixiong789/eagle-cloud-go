# 开发环境部署

本文只说明本地开发、联调和 IDEA 运行入口。生产镜像、Kubernetes 发布顺序和回滚见
[生产环境部署](deployment.md)，线上故障处置见[生产运行手册](operations.md)。

## 两种开发方式

| 方式 | 适用场景 | 应用进程 | 基础依赖 |
|---|---|---|---|
| 完整 Compose | 首次启动、接口联调、验证完整拓扑 | 容器内运行 | Compose 管理 |
| 宿主机调试 | 断点、热重启、单服务开发 | IDEA/终端运行 | Compose 只启动依赖 |

Compose 中的 `admin-migrate`、`product-migrate`、`order-migrate` 是一次性数据库迁移任务，
对应的 `admin`、`product`、`order` 才是常驻服务。迁移容器显示 `Exited (0)` 表示成功，
不是服务异常或重复部署。

开发 Compose 的 nginx 提供 `http://127.0.0.1:8000` 入口。生产按需使用独立
[Compose + nginx 清单](compose-deployment.md)或 K3s + Envoy Gateway；开发凭据和调试端口
不进入生产清单。开发/生产 nginx 共用路径规则和 Docker DNS 动态解析配置。

## 依赖版本基线

2026-10-04 核对官方稳定发布；Go 依赖与开发工具由各模块 `go.mod`/`go.sum` 锁定，CI Actions 固定到对应稳定标签的完整提交。容器基线：

| 组件 | 版本 | 官方发布 |
|---|---|---|
| Go / builder | 1.27.1 | [Go](https://go.dev/dl/) |
| 运行镜像 | distroless static-debian13:nonroot | [Distroless](https://github.com/GoogleContainerTools/distroless) |
| PostgreSQL / 备份客户端 | 18.6 | [PostgreSQL](https://www.postgresql.org/docs/release/) |
| Keycloak | 26.8.0 | [Keycloak](https://www.keycloak.org/downloads.html) |
| Redis | 8.10.2 | [Redis](https://download.redis.io/releases/) |
| RabbitMQ | 4.3.6 | [RabbitMQ](https://www.rabbitmq.com/release-information) |
| nginx | 1.30.5（stable 分支） | [nginx](https://nginx.org/) |
| Prometheus / Alertmanager | 3.15.0 / 0.34.1 | [Prometheus](https://github.com/prometheus/prometheus/releases), [Alertmanager](https://github.com/prometheus/alertmanager/releases) |
| Tempo / Loki / Alloy | 3.1.0 / 3.7.8 / 1.20.1 | [Tempo](https://github.com/grafana/tempo/releases), [Loki](https://github.com/grafana/loki/releases), [Alloy](https://github.com/grafana/alloy/releases) |
| Grafana / Redis exporter | 13.2.3 / 1.93.0 | [Grafana](https://github.com/grafana/grafana/releases), [exporter](https://github.com/oliver006/redis_exporter/releases) |
| OTel Collector contrib | 默认 0.161.0 | [Collector](https://github.com/open-telemetry/opentelemetry-collector-releases/releases) |
| AWS CLI（数据库备份） | 2.37.9 | [AWS CLI](https://raw.githubusercontent.com/aws/aws-cli/v2/CHANGELOG.rst) |
| Envoy Gateway / K3s | 1.9.2 / 1.36.5+k3s1 | [兼容范围说明](../deploy/kubernetes/k3s/README.md#1-初始化-k3s) |

Buf 1.73.0 仍依赖旧版 LSP API；`go.lsp.dev/protocol` / `jsonrpc2` / `uri` 分别固定为 0.12.0 / 0.10.0 / 0.3.0。1.0.1 的接口变化会使最新版 Buf 编译失败，待上游适配后再升级这些开发工具的间接依赖。golangci-lint 2.14.0 同样按上游 API 约束保留 exhaustive 0.13.0、predeclared 0.2.2 和 glob 0.2.3；更高版本删除了主工具仍使用的接口。Casbin 已迁移到 v3.11.0，鉴权一致性测试随全量测试执行。

Collector 0.162.0 已有 `-arm64`、`-amd64` 等平台标签，但截至核对时没有通用多架构标签，因此默认使用最新可拉取的多架构稳定标签 0.161.0。确认部署平台后，可在 `.env` 中把 `EAGLE_OTEL_IMAGE_TAG` 改为 `0.162.0-arm64` 或 `0.162.0-amd64`。K3s 则按 Envoy Gateway 已声明的 Kubernetes 支持范围固定版本；预发布、nightly 不纳入本基线。

## IDEA 从 Markdown 直接运行

README 和本文中的可执行命令都使用 `bash` 代码块，并遵循“一块一个动作”。IDEA/GoLand 启用
Markdown 和 Shell Script 插件后，代码块左侧会显示运行按钮，可以直接点击执行。

命令默认以仓库根目录为工作目录。需要共享 shell 变量的步骤会合并为一个原子命令；需要持续
交互或手工修改参数的生产命令不伪装成本地一键操作。

## 完整 Compose 环境

首次使用先验证项目锁定的开发工具：

```bash
make init
```

先按下文[对象存储配置](#对象存储配置)准备根目录 `.env`。默认 OSS 的配置缺失时 admin 会明确报错退出。已有 PostgreSQL 17 数据请先完成[数据升级](#已有-postgresql-17-数据的升级)。

启动完整开发拓扑：

```bash
make up
```

查看常驻服务和一次性任务：

```bash
docker compose -f deploy/docker-compose.yml ps --all
```

预期结果：

- 三个 `*-migrate` 为 `Exited (0)`；
- `admin`、`product`、`order`、PostgreSQL、Redis、RabbitMQ 为运行状态；
- `gateway` 在三个业务服务健康后启动。

检查三个服务的 readiness：

```bash
curl --fail http://127.0.0.1:9101/readyz
```

```bash
curl --fail http://127.0.0.1:9102/readyz
```

```bash
curl --fail http://127.0.0.1:9103/readyz
```

查看某个服务日志：

```bash
docker compose -f deploy/docker-compose.yml logs --follow admin
```

停止环境但保留数据库和中间件数据：

```bash
make down
```

`docker compose down -v` 会删除本地数据库、对象和队列数据，不作为入门文档的一键命令。
确实需要重置环境时，应先确认没有要保留的数据。

## 本地入口

| 组件 | 地址 | 本地凭据 |
|---|---|---|
| nginx 开发网关 | `http://127.0.0.1:8000` | - |
| Keycloak | `http://127.0.0.1:8080` | 管理员 `admin/admin` |
| RabbitMQ 管理台 | `http://127.0.0.1:15672` | `eagle/eagle` |
| Grafana（obs profile） | `http://127.0.0.1:3000` | 匿名 Admin |
| Prometheus（obs profile） | `http://127.0.0.1:9090` | - |

业务服务调试端口：

| 服务 | HTTP | gRPC | metrics/health |
|---|---:|---:|---:|
| admin | 8001 | 9001 | 9101 |
| product | 8002 | 9002 | 9102 |
| order | 8003 | 9003 | 9103 |

容器之间使用 `dns:///admin:9000`、`dns:///product:9000` 等 Compose DNS；宿主机进程使用
`127.0.0.1:9001`、`127.0.0.1:9002`。

## 宿主机断点调试

先只启动 PostgreSQL、Keycloak、Redis、RabbitMQ：

```bash
make up-deps
```

迁移 admin 数据库：

```bash
make migrate-up SERVICE=admin
```

从 Markdown 运行或在终端启动 admin：

```bash
EAGLE_FILE_PROVIDER=local make run SERVICE=admin
```

调试 product 前先保证 admin 正在运行，然后迁移并启动 product：

```bash
make migrate-up SERVICE=product
```

```bash
make run SERVICE=product
```

调试 order 前先保证 product 正在运行，然后迁移并启动 order：

```bash
make migrate-up SERVICE=order
```

```bash
make run SERVICE=order
```

三个服务的本地配置分别位于 `app/<service>/configs/config.yaml`。宿主机运行使用其中的
`127.0.0.1` 默认值；容器运行则由 Compose 的 `EAGLE_*` 环境变量覆盖为容器 DNS。

不要同时运行同一个服务的 Compose 容器和宿主机进程，否则 HTTP、gRPC 和 metrics 端口会冲突。
需要断点调试某服务时，可以先停止对应容器：

```bash
docker compose -f deploy/docker-compose.yml stop admin
```

## 对象存储配置

默认使用阿里云 OSS 官方 Go SDK V2，采用 V4 签名和 HTTPS；不再启动本地 MinIO。先复制配置模板：

```bash
cp .env.example .env
```

创建私有 Bucket 后，在 `.env` 中填写 `EAGLE_FILE_OSS_REGION`、`EAGLE_FILE_OSS_BUCKET`、`EAGLE_FILE_OSS_ACCESS_KEY_ID` 和 `EAGLE_FILE_OSS_ACCESS_KEY_SECRET`。地域例如 `cn-hangzhou`，必须与 Bucket 一致。RAM 身份只需对该 Bucket 的对象授予 `oss:PutObject`、`oss:GetObject`、`oss:DeleteObject`，适配器不会创建或列举 Bucket。Endpoint 留空时 SDK 推导 HTTPS 公网地址；同地域服务可覆盖为 OSS internal endpoint。

使用 STS 时还需填写 `EAGLE_FILE_OSS_SECURITY_TOKEN`。当前凭据在启动时读取，临时凭据到期前应更新配置并重启 admin。SDK 用法见[阿里云官方文档](https://www.alibabacloud.com/help/en/oss/developer-reference/manual-for-go-sdk-v2/)。

尚未创建 OSS 时，把 `.env` 的 `EAGLE_FILE_PROVIDER` 改为 `local`；Compose 使用持久化 `uploads` 卷。也可以临时覆盖：

```bash
EAGLE_FILE_PROVIDER=local make up
```

宿主机 `make run` 不自动读取 `.env`。使用 OSS 调试时在同一个 shell 中导出配置再启动：

```bash
set -a; source .env; set +a; make run SERVICE=admin
```

使用本地存储调试时：

```bash
EAGLE_FILE_PROVIDER=local make run SERVICE=admin
```

`local` 适合单实例开发；多副本部署使用 OSS。原有 `s3` 适配器保留为显式可选项，变量为 `EAGLE_FILE_S3_*`，不会自动切换或迁移已有对象。此前 MinIO 对象需另行迁移；原卷不会被本次配置变更删除。

## 已有 PostgreSQL 17 数据的升级

PostgreSQL 18 官方镜像改变了数据目录布局，当前挂载 `pgdata18:/var/lib/postgresql`，与旧 `pgdata` 卷分开。已有环境不能仅修改镜像标签并复用旧数据目录；新卷为空，须导出、恢复后再启动业务。

在升级前停止写入，使用旧版本 Compose 停止应用、网关、Keycloak，保留 PostgreSQL 17 运行。将项目的四个数据库导出为 custom 格式（包含敏感数据，保存到仓库外；目录名请使用本次升级的唯一名称）：

```bash
umask 077; mkdir /tmp/eagle-pg17-backup && for db in eagle_admin eagle_product eagle_order keycloak; do docker compose -f deploy/docker-compose.yml exec -T postgres pg_dump -U eagle -Fc "$db" > "/tmp/eagle-pg17-backup/$db.dump" || exit 1; done
```

导出成功后，使用旧 Compose 停止 PostgreSQL，再切换当前配置，仅启动 PostgreSQL 18：

```bash
docker compose -f deploy/docker-compose.yml up -d postgres
```

等待健康后恢复到初始化脚本创建的空数据库。以下 `--clean` 会清理目标库同名对象，只对本次升级的新卷执行：

```bash
for db in eagle_admin eagle_product eagle_order keycloak; do docker compose -f deploy/docker-compose.yml exec -T postgres pg_restore -U eagle -d "$db" --clean --if-exists --exit-on-error < "/tmp/eagle-pg17-backup/$db.dump" || exit 1; done
```

检查业务数据、Keycloak 和迁移版本后再执行 `make up`。项目使用统一的开发角色 eagle；若旧库包含自建角色或额外数据库，请另行导出和恢复。保留旧卷和导出文件直到验证完成。生产数据库按[PostgreSQL 官方升级说明](https://www.postgresql.org/docs/current/upgrading.html)单独规划升级；备份任务的客户端版本已同步到 18。

## 可观测性联调

在完整环境基础上启动可观测性 profile：

```bash
docker compose -f deploy/docker-compose.yml --profile obs up -d
```

该 profile 启动 Prometheus、Alertmanager、Tempo、Loki、Alloy、Grafana 和 OTel Collector。
Alloy 从 Docker stdout 收集结构化日志；应用将 trace 发到 OTel Collector。没有启动 profile 时，
OTLP exporter 后台重试不会阻止业务服务启动。

## 首个本地用户

登录 Keycloak 管理 CLI：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh config credentials --server http://localhost:8080 --realm master --user admin --password admin
```

创建用户：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh create users -r eagle -s username=alice -s enabled=true -s firstName=Alice -s lastName=Test -s email=alice@example.com
```

设置密码：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh set-password -r eagle --username alice --new-password 'Passw0rd!'
```

授予开发管理员角色：

```bash
docker compose -f deploy/docker-compose.yml exec -T keycloak /opt/keycloak/bin/kcadm.sh add-roles -r eagle --uusername alice --rolename admin
```

获取 token 并调用权限接口。这个代码块是一个原子动作，避免 IDEA 分别运行代码块时丢失
shell 变量：

```bash
TOKEN=$(curl --silent --fail -d client_id=eagle-web -d username=alice -d 'password=Passw0rd!' -d grant_type=password http://127.0.0.1:8080/realms/eagle/protocol/openid-connect/token | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p') && curl --fail -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8000/v1/system/permissions
```

## 常见问题

### `*-migrate` 一直运行或退出码非 0

正常迁移会很快结束并显示 `Exited (0)`。先查看日志：

```bash
docker compose -f deploy/docker-compose.yml logs admin-migrate
```

常见原因是 PostgreSQL 尚未健康、初始化卷来自旧版本，或迁移 SQL 失败。不要绕过迁移任务强行
启动服务。

### Markdown 代码块没有运行按钮

确认项目是从仓库根目录打开，并启用了 Markdown 和 Shell Script 插件。代码块语言必须是
`bash`；纯文本、YAML、配置示例和生产占位命令不会提供一键执行入口。

### 服务启动后接口仍不可用

先检查 `/readyz`，再检查对应服务日志。product 依赖 admin，order 依赖 product；上游未就绪时，
下游即使进程存活也不能完成完整调用链。
