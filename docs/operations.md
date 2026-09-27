# 生产运行手册

本文只说明生产控制面和故障处置；本地启动见[开发环境部署](development-deployment.md)，
服务边界见[架构说明](architecture.md)，生产发布顺序见[生产环境部署](deployment.md)。

## 发布与回滚

推送 `v*` tag 或手动运行 `Release images` workflow。流水线先编译、测试并渲染全部
部署组合，再为 admin、product、order 分别构建 amd64/arm64 镜像，生成 SBOM 和
provenance、执行 HIGH/CRITICAL 漏洞扫描，最后输出不可变 digest。

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

order 在业务事务内写 Outbox，relay 使用 publisher confirm 投递；admin 消费端使用 Inbox
去重。失败采用指数退避，20 次仍失败会写入 `failed_at` 停止自动重试，避免毒消息持续冲击。
发布成功记录保留 7 天，Inbox 保留 30 天。重要业务若允许超过 30 天重复投递，应提高 Inbox
保留期。

排查停放的 Outbox：

```bash
EAGLE_DATABASE_DSN='postgres://...' ./bin/outboxctl -command list
EAGLE_DATABASE_DSN='postgres://...' ./bin/outboxctl -command retry -id EVENT_ID -yes
```

先定位 broker、schema、下游或数据问题并修复，再重试。不要批量直接改表。检查 DLQ 不会 ack
消息；显式 redrive 使用 publisher confirm，成功后才 ack 原消息：

```bash
EAGLE_MESSAGING_RABBITMQ_URL='amqps://...' ./bin/mqctl \
  -command inspect -queue eagle.admin.order-created.v1.dlq
EAGLE_MESSAGING_RABBITMQ_URL='amqps://...' ./bin/mqctl \
  -command redrive -queue eagle.admin.order-created.v1.dlq \
  -exchange eagle.events -routing-key order.created.v1 -limit 20 -yes
```

Redrive 前确认消费者已向前兼容事件版本。RabbitMQ 生产集群应启用 quorum queue、跨可用区
副本、磁盘/内存水位告警和 definitions 备份；这些属于 broker 平台配置，不由应用启动时创建。

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
