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

生产使用 Envoy Gateway；Compose 中的 nginx 只是轻量开发网关，为本地提供统一的
`http://127.0.0.1:8000` 入口，两者不会部署在同一个环境。

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

启动完整开发拓扑：

```bash
make up
```

查看常驻服务和一次性任务：

```bash
docker compose -f deploy/docker-compose.yml ps --all
```

预期结果：

- 三个 `*-migrate` 和 `minio-init` 为 `Exited (0)`；
- `admin`、`product`、`order`、PostgreSQL、Redis、RabbitMQ、MinIO 为运行状态；
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
| MinIO Console | `http://127.0.0.1:9005` | `eagle/eagle-local-secret` |
| Grafana（obs profile） | `http://127.0.0.1:3000` | 匿名 Admin |
| Prometheus（obs profile） | `http://127.0.0.1:9090` | - |

业务服务调试端口：

| 服务 | HTTP | gRPC | metrics/health |
|---|---:|---:|---:|
| admin | 8001 | 9001 | 9101 |
| product | 8002 | 9002 | 9102 |
| order | 8003 | 9003 | 9103 |

容器之间使用 `admin:9000`、`product:9000` 等 Compose DNS；宿主机进程使用
`127.0.0.1:9001`、`127.0.0.1:9002`。

## 宿主机断点调试

先只启动 PostgreSQL、Keycloak、Redis、RabbitMQ 和 MinIO：

```bash
make up-deps
```

迁移 admin 数据库：

```bash
make migrate-up SERVICE=admin
```

从 Markdown 运行或在终端启动 admin：

```bash
make run SERVICE=admin
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
