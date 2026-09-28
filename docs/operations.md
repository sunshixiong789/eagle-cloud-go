# 生产运行手册

本文只说明生产控制面和故障处置；本地启动见[开发环境部署](development-deployment.md)，
服务边界见[架构说明](architecture.md)，生产发布顺序见[生产环境部署](deployment.md)。

## 发布与回滚

推送 `v*` tag 或手动运行 [Release images](../.github/workflows/release.yml)。流水线先运行
[CI](../.github/workflows/ci.yml) 的生成差异、兼容性、编译、真实依赖测试和部署渲染检查，
再为 admin、product、order 分别构建 amd64/arm64 镜像，生成 SBOM 和 provenance。
候选镜像先上传 GHCR 的 `candidate-*` 标签，两个架构的 HIGH/CRITICAL 漏洞扫描均通过后，
才发布版本标签并输出不可变 digest；候选标签不得部署，失败候选由 registry 生命周期策略清理。
手动触发使用 `sha-<commit>` 版本。需在仓库配置 packages 写权限、分支保护和 `release` environment；
生产 overlay 仍由环境仓库更新，这些工作流不直接部署生产，也不代表已安装镜像签名准入策略。

镜像发布与环境部署刻意分离。生产由 GitOps 仓库把 overlay 中的镜像改为 digest，
按“expand migration Job → Deployment 滚动 → 指标观察 → contract migration”推进。
回滚应用只回退 digest；已经执行的数据库变更必须保持向后兼容，不能随 Pod 自动回滚。

上线后至少观察 30 分钟：可用性错误预算、p99、Pod 重启、Outbox 年龄、DLQ、消费失败。
快速错误预算告警触发时停止发布；不要在错误预算耗尽时继续常规变更。

## 生产网关与服务通信

应用基线定义路由；`deploy/kubernetes/gateway` 为 Envoy Gateway 增加 TLS 1.2+、请求/
连接超时、连接上限、本地限流、least-request 负载均衡与后端熔断。域名、CORS origin、
GatewayClass 和容量阈值必须在环境 overlay 中按压测数据调整。应用级重试只用于幂等查询，
不要对创建订单等写请求在网关层自动重试。

内部 gRPC 通过 Headless Service + `dns:///` 做客户端请求级均衡。需要加密的环境部署：

```bash
kubectl apply -k deploy/kubernetes/overlays/production-mtls
```

该 overlay 依赖 Istio，gRPC 9000 使用 STRICT mTLS，并用 ServiceAccount 限制 product→admin、
order→product。HTTP 8000 和 metrics 9100 仍由 Gateway/Prometheus 访问，因此保留 PERMISSIVE；
NetworkPolicy 同时做 L3/L4 限制。数据库、RabbitMQ、Redis、OIDC 和 S3 的生产连接也必须使用
各自 TLS 地址与受信 CA，服务网格不会替代外部依赖的 TLS。

## MQ 一致性和运维闭环

order 在业务事务内写 Outbox，relay 使用 mandatory 发布并同时检查 return 与 publisher confirm。
不存在队列绑定时按失败处理、保留 Outbox 重试；每次发布含连接与确认等待最多 5 秒，短于 30 秒租约。
首次部署应先创建消费拓扑或启动消费者，再开放业务流量。admin 消费端使用 Inbox
去重。失败采用指数退避，20 次仍失败会写入 `failed_at` 停止自动重试，避免毒消息持续冲击。
发布成功记录保留 7 天，Inbox 保留 30 天。重要业务若允许超过 30 天重复投递，应提高 Inbox
保留期。

排查停放的 Outbox：

```bash
EAGLE_DATABASE_DSN='postgres://...' ./bin/outboxctl -command list
EAGLE_DATABASE_DSN='postgres://...' ./bin/outboxctl -command retry -id EVENT_ID -yes
```

先定位 broker、schema、下游或数据问题并修复，再重试。不要批量直接改表。检查 DLQ 不会 ack
消息；显式 redrive 复用 mandatory/return/confirm 发布器，确认已路由后才 ack 原消息：

```bash
EAGLE_MESSAGING_RABBITMQ_URL='amqps://...' ./bin/mqctl \
  -command inspect -queue eagle.admin.order-created.v2.dlq
EAGLE_MESSAGING_RABBITMQ_URL='amqps://...' ./bin/mqctl \
  -command redrive -queue eagle.admin.order-created.v2.dlq \
  -exchange eagle.events -routing-key order.created.v1 -limit 20 -yes
```

消费中正常停机或取消上下文时关闭 channel，由 broker 重入队，不把停机当作毒消息送入 DLQ。
Redrive 前确认消费者已向前兼容事件版本。应用声明主队列和 DLQ 为 quorum queue，主队列使用
`x-dead-letter-strategy=at-least-once` 和 `x-overflow=reject-publish`。DLQ 暂时不可路由时，
消息保留在源队列，由 broker 后台重试；恢复后的投递可能延迟数分钟，也可能重复，消费者仍须幂等。
生产需要另外配置至少三个 RabbitMQ 节点和跨可用区副本布局、磁盘/内存水位告警及 definitions 备份；
单节点 quorum queue 不代表高可用。

### 从旧 classic 队列迁移

默认队列改为 `eagle.admin.order-created.v2`，事件 routing key 仍为 `order.created.v1`。
RabbitMQ 不允许原地更改队列类型；不要将新版本的队列名覆盖为旧 classic 队列名。
已有部署使用维护窗口迁移（首次部署无需执行）：

1. 暂停所有订单事件发布进程，保留旧 admin 消费者，等待旧主队列 ready/unacked 均为 0。
2. 检查旧 `eagle.admin.order-created.v1.dlq`，修复并 redrive 待处理事件，再确认两个旧队列均为空。
   无法处理的消息应先导出保留，不能为迁移直接丢弃。
3. 停止旧 admin，解除旧主队列在 `eagle.events` 的绑定，以及旧 DLQ 在 `eagle.events.dlx` 的绑定。
   空旧队列可保留供核对；不要让旧版本重新启动并恢复绑定。
4. 启动新 admin，确认新主队列及 DLQ 为 quorum、消费者已连接，再恢复订单发布并验证通知。

`EAGLE_MESSAGING_EXCHANGE` 和 `EAGLE_MESSAGING_ORDER_CREATED_QUEUE` 可指定隔离环境的拓扑；
共享同一事件流的服务必须使用同一 exchange。迁移失败时先暂停发布，核对新旧队列积压再决定回滚，
不要直接切回旧消费者并遗留新队列消息。

## 可观测性与 SLO

本地 `--profile obs` 会启动 Prometheus、Alertmanager、Tempo、Loki、Alloy 和 Grafana，并自动
加载 `Eagle production overview`。Kubernetes 的 `observability` kustomization 提供
ServiceMonitor、PrometheusRule、Grafana dashboard ConfigMap 和 AlertmanagerConfig。

应用前创建真实 webhook Secret，并确认 Prometheus/Alertmanager/Grafana sidecar 的 selector
会选中这些对象：

```bash
cp deploy/kubernetes/observability/alert-webhook-secret.example.yaml /tmp/eagle-alert.yaml
kubectl apply -f /tmp/eagle-alert.yaml
kubectl apply -k deploy/kubernetes/observability
```

生产 SLO 为 30 天 99.9% HTTP 可用性，配置 fast/slow burn 告警；另监控 p99、服务不可用、
Outbox 停放/延迟、DLQ、消费失败、队列积压、Redis 和备份失败。告警必须进入有人值守的通知
渠道，并按季度做一次“触发→通知→确认→恢复”的演练。日志只写结构化 stdout，由平台收集；
日志、指标、trace 使用同一 `service_name`、trace ID 和发布时间标签关联。

## 备份、恢复与容灾

优先使用托管 PostgreSQL 的 PITR、跨可用区高可用和跨区域副本。仓库中的 CronJob 是便携
兜底：三个数据库错峰执行 `pg_dump -Fc`，用 `pg_restore --list` 校验后，以服务端加密上传
到 S3 兼容存储。

```bash
kubectl apply -k deploy/kubernetes/backup
```

部署前修改 `backup/configmap.yaml`，通过 External Secrets 创建
`eagle-backup-credentials`，并在 bucket 配置版本控制、不可变保留、生命周期和异地复制。
`RETENTION_DAYS` 是平台生命周期策略的声明值，CronJob 不持有删除权限，也不会主动删除备份。

每月至少恢复一次到隔离数据库并执行迁移、核心读接口和数据量核对。恢复模板不在
kustomization 中，复制 `restore-job.example.yaml`，明确修改数据库、对象 URI，并把
`RESTORE_CONFIRMED` 改为 `yes` 后才可应用。生产建议目标：数据库 RPO ≤ 15 分钟（PITR）、
RTO ≤ 60 分钟；对象存储 RPO 由供应商复制策略定义。每半年做一次区域级演练并记录实际 RTO。

## K3s 上线检查

- 使用 `production-k3s` overlay：restricted Pod Security、非 root、只读根文件系统、最小权限
  ServiceAccount、PDB、HPA、topology spread、ResourceQuota、LimitRange、PriorityClass。
- 三个 server/etcd 节点保持 Ready；一次只维护一台，恢复 quorum 和工作负载后再处理下一台。
- 控制面 LB 健康检查三个节点 6443，业务 LB 健康检查三个节点 80/443，不能共享单点入口。
- K3s etcd snapshot 复制到集群之外并定期恢复验证；local-path 卷不承载生产数据库高可用。
- 使用支持 NetworkPolicy 的 CNI；按实际数据库、OIDC、MQ、对象存储地址补环境级 egress 白名单。
- Secret 由 External Secrets/Vault 提供并轮换，禁止把填值后的示例提交到仓库。
- 所有镜像以 digest 部署，集群启用准入策略校验签名/provenance 和禁止特权容器。
- HPA、限流、熔断、资源 requests/limits 必须用容量测试校准，不把示例阈值直接视为生产容量。

## 授权和缓存故障边界

`EAGLE_UPSTREAM_AUTHORIZATION_MAX_STALENESS` 控制策略有效期，默认 `15s`，必须不小于刷新周期。
超过有效期，普通权限请求拒绝；内部白名单请求与既有超管角色语义不变。修改该参数前记录可接受的
撤权延迟；延长有效期会延长旧授权可用窗口。恢复后同版本成功确认即可恢复服务，倒退版本不延长有效期。

生产 Redis 默认启用 TLS。公共 CA 使用系统根；私有 CA 通过只读 Secret 卷挂载并设置
`EAGLE_CACHE_REDIS_TLS_CA_FILE`。`TLS_SERVER_NAME` 必须是证书 SAN 中的名称，不提供跳过验签选项。
CA、密码和服务凭据修改后滚动重启实例；当前客户端不声称支持凭据热加载。
可用 `EAGLE_CACHE_REDIS_ENABLED=false` 临时关闭缓存。恢复缓存后观察数据库负载回落、延迟和错误日志。
