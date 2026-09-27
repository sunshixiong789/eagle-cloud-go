# 生产环境部署

本文以三节点 K3s HA 作为默认生产模式，描述生产制品、平台前置条件和安全发布顺序。本地
Compose、宿主机调试和 IDEA 入口见
[开发环境部署](development-deployment.md)，告警、MQ、备份恢复和故障处置见
[生产运行手册](operations.md)。

## 发布单元

模块不等于微服务。当前六个业务模块组合为三个独立发布单元：

| 服务 | 业务模块 | 镜像 | 独占数据库 |
|---|---|---|---|
| admin | access、dictionary、file、notification | `eagle/admin:<version>` | `eagle_admin` |
| product | product | `eagle/product:<version>` | `eagle_product` |
| order | order | `eagle/order:<version>` | `eagle_order` |

每个镜像只包含当前服务的 `/app/service`、通用 `/app/migrate`、`/app/healthcheck`、配置和
`app/<service>/migrations`。三个服务分别拥有 Deployment、Service、数据库账号和发布节奏；
禁止跨服务共享数据库、迁移目录或事务。

## 构建不可变镜像

仓库使用参数化多阶段 Dockerfile，但每次只构建一个服务。正式发布通过 `Release images`
GitHub Actions workflow 生成 amd64/arm64 镜像、SBOM、provenance 和漏洞扫描结果。

发布 tag：

```bash
git tag v1.2.0
```

```bash
git push origin v1.2.0
```

也可以在发布平台手动触发 workflow 并提供唯一版本号。环境仓库必须使用 workflow 输出的
镜像 digest，例如 `ghcr.io/example/eagle/admin@sha256:...`，不要使用 `stable`、`staging`
或 `latest` 等可变标签。仓库 overlay 中的仓库名和标签仅为可渲染示例，不能直接用于生产。

本地验证单服务镜像时可以执行：

```bash
make image SERVICE=product VERSION=v1.2.0 REGISTRY=registry.example.com/eagle
```

## 平台前置条件

默认拓扑是三台 K3s server 同时承担 control-plane、embedded etcd 和 worker。三个节点必须使用
SSD 和稳定私网，任意两台的剩余容量能够承载核心工作负载；该拓扑容忍一个节点故障，但不容忍
同时失去两个节点。建群和入口配置见
[三节点 K3s 生产集群](../deploy/kubernetes/k3s/README.md)。

应用进入集群前，平台必须已经提供：

| 能力 | 要求 |
|---|---|
| K3s | 三个 server/etcd/worker 节点、固定版本、SSD、etcd 异地 snapshot |
| 控制面入口 | 固定内网 DNS/VIP 或 LB，将 `6443` 健康转发到三个 server |
| 边缘入口 | 外部 LB 将 `80/443` 转发到三个节点的 K3s ServiceLB |
| 边缘网关 | 固定版本 Envoy Gateway 控制器、Gateway API 与 Envoy Gateway CRD |
| 身份 | 生产 Keycloak/OIDC、独立服务身份和 Client Credentials |
| 数据 | 三个独立 PostgreSQL database 与最小权限账号 |
| 中间件 | 高可用 Redis、RabbitMQ、S3 兼容对象存储 |
| 可观测性 | OTel Collector、Prometheus Operator、日志和 trace 后端 |
| Secret | External Secrets、Vault 或云 Secret Manager |

仓库不会在生产应用命名空间部署单副本 PostgreSQL、Redis、RabbitMQ、Keycloak 或对象存储。

## 环境 Overlay

`deploy/kubernetes/base` 定义三个服务的 Deployment/Service、Gateway API、HPA、PDB、探针、
安全上下文和 NetworkPolicy。迁移 Job 模板按服务拆分在 `deploy/kubernetes/migrations/`，
不属于应用 base，避免与 Deployment 同时 apply。`production-k3s` 组合通用 `production` 基线，
并增加三副本 Envoy 数据面和 `GatewayClass`，是默认生产入口。实际环境必须在独立 GitOps 配置
中替换以下内容：

- 三个镜像的仓库和 digest；
- API 域名、TLS Secret、GatewayClass 和 CORS origin；
- OIDC issuer、JWKS、服务间 token 地址；
- 三个数据库 DSN、Redis、RabbitMQ 和 S3 地址及凭据；
- 资源、HPA、限流、熔断和超时参数；
- OTLP、Prometheus、日志与告警接收方。

只做静态渲染检查：

```bash
make validate-deploy
```

该命令只证明 Compose 和 Kustomize 可以解析，不证明生产依赖存在，也不执行迁移或发布。
只查看默认生产结果可执行 `make render-prod-k3s`。

## 迁移与滚动发布

Deployment 启动时不会自动迁移，避免多副本并发修改 schema。每次发布必须由部署平台按服务
串行编排，不能依赖一次 `kubectl apply -k` 自动保证 Job 与 Deployment 的先后顺序。

单个服务的发布状态机是：

```text
确认不可变镜像 digest
  → 创建本次发布唯一名称的 migration Job
  → 等待 Job Complete
  → 更新同一服务 Deployment 的镜像 digest
  → 等待 rollout 完成和 /readyz 通过
  → 观察指标与日志
```

发布平台必须先选择发生变化的服务、替换镜像 digest，并为模板名称追加本次发布标识，再用
create 语义创建。迁移 Job 必须满足：

- 名称包含服务和发布标识，例如 `admin-migrate-a613c46`；
- 使用与即将发布的 Deployment 完全相同的镜像 digest；
- 只挂载当前服务的数据库 DSN；
- 命令为 `/app/migrate -dir /app/migrations`；
- 失败时阻止 Deployment 更新；
- 成功记录至少保留到发布观察期结束，再由 TTL 或平台清理。

固定名称的旧 Job 不能复用：Kubernetes Job PodTemplate 不可修改，复用还可能导致新迁移没有执行。
`admin-migrate`、`product-migrate`、`order-migrate` 是模板角色名，发布平台必须为每次运行生成唯一
对象名。

只发布发生变化的服务。例如 product 发布不应重新运行 admin/order 迁移，也不应滚动其 Pod。

## 数据库变更策略

数据库迁移采用 expand/contract：

1. expand：先添加兼容的新表、列、索引或双写能力；
2. rollout：发布仍兼容旧 schema 的应用；
3. backfill：限速回填并核对数据；
4. contract：确认所有旧版本退出后，再删除旧结构。

应用回滚只回退镜像 digest；已经执行的 schema 变更不得依赖 Pod 自动回滚。破坏性 contract
迁移必须作为独立发布，在备份、兼容窗口和回滚方案确认后执行。

## 网关与服务通信

生产只使用 Gateway API + Envoy Gateway，不部署 Compose 的 nginx。K3s 初始化时禁用默认
Traefik，保留 ServiceLB；ServiceLB 在三个节点承接 80/443，外部 LB 负责节点健康检查。
Envoy Gateway 控制器通过固定版本 Helm chart 安装，`production-k3s` 创建
`GatewayClass/envoy`、`Gateway`、`HTTPRoute` 和三副本 Envoy 数据面。

外部流量按路径进入三个 HTTP Service：

| 路径 | 后端 |
|---|---|
| `/v1/system` | admin:8000 |
| `/v1/products` | product:8000 |
| `/v1/orders` | order:8000 |

内部 gRPC 使用 Headless Service + `dns:///`：product 调 admin，order 调 product。服务间调用
使用独立 Client Credentials，不能复用 Compose 的 `eagle-worker` 开发凭据。

## Secret 与配置

`deploy/kubernetes/secret.example.yaml` 只列出键名，所有值都是 `CHANGE_ME`，不得直接 apply。
生产由 Secret Manager 生成同名 `eagle-runtime` Secret。普通配置通过 ConfigMap 注入，敏感值
只能通过 Secret 引用。

上线前至少核对：

- token 的 `iss` 与 `EAGLE_AUTH_ISSUER` 完全一致；
- product/order 使用不同的 Client Credentials；
- 每个 DSN 只能访问自己的 database；
- RabbitMQ、Redis、OIDC、S3、OTLP 使用生产 TLS 地址和受信 CA；
- TLS Secret、数据库口令、服务 secret 支持独立轮换。

## 就绪、扩缩容与网络

`/livez` 只表示进程存活，`/readyz` 聚合初始化状态和数据库等依赖检查。Gateway 和 Service 只应
把流量发送给 readiness 通过的 Pod。滚动策略保持 `maxUnavailable: 0`，发布平台仍需等待
Deployment rollout，而不是只等待资源 apply 成功。

K3s ServiceLB 本身不代替外部 LB 的节点健康检查。外部 LB 必须在节点或 Envoy 不健康时停止向
该节点转发；控制面 6443 与业务 80/443 使用独立后端池和健康策略。

HPA、PDB、topology spread、ResourceQuota、LimitRange、网关限流和熔断值都是基线，不是生产
容量结论，必须由压测和真实流量校准。NetworkPolicy 只允许网关访问 HTTP、指定服务访问内部
gRPC、Prometheus 访问 metrics；环境仓库还应按实际外部依赖补 egress 白名单。

## 上线检查

- 镜像使用 digest，签名/provenance 校验通过；
- 三个 K3s 节点、embedded etcd 和控制面固定入口健康，异地 snapshot 可恢复；
- 只运行发生变更服务的唯一迁移 Job，且状态为 Complete；
- Deployment 已更新到同一 digest，rollout 和 readiness 通过；
- Gateway 状态为 Programmed，Envoy 控制面/数据面跨节点分布，证书、域名和 CORS 正确；
- Secret 来自受控系统，示例 Secret 未被应用；
- 资源、HPA、限流和熔断已经按环境容量校准；
- 错误率、p99、Pod 重启、Outbox 年龄、DLQ 和消费失败进入观察面板；
- 发布负责人、回滚条件和观察窗口已经记录。
