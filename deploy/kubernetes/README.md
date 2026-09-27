# Kubernetes 部署清单

本目录提供应用层生产基线和发布模板。完整的生产前置条件、迁移顺序、网关与上线检查见
[生产环境部署](../../docs/deployment.md)，故障处置见[生产运行手册](../../docs/operations.md)。

## 目录边界

| 目录 | 内容 | 发布方式 |
|---|---|---|
| `base/` | Deployment、Service、Gateway API、HPA、PDB、探针、NetworkPolicy | 由环境 overlay 引用 |
| `migrations/` | 三个服务的一次性 Job 模板 | 每次发布选择服务、替换 digest 后 create |
| `overlays/staging/` | staging 可渲染示例 | 复制到环境仓库后定制 |
| `overlays/production/` | production 可渲染示例 | 复制到环境仓库后定制 |
| `overlays/production-k3s/` | 三节点 K3s production 入口 | 默认生产部署模式 |
| `overlays/production-mtls/` | production + Istio mTLS | 替代普通 production overlay |
| `k3s/` | K3s server 配置、Envoy Gateway values 和建群说明 | 集群初始化与升级时使用 |
| `gateway/` | Envoy Gateway 流量、安全和弹性策略 | 与应用 overlay 一起管理 |
| `observability/` | Prometheus Operator 资源 | 平台已安装 CRD 后应用 |
| `backup/` | PostgreSQL 便携备份兜底 | 托管 PITR 不可用时采用 |

`base/` 不包含迁移 Job，也不会部署 PostgreSQL、Redis、RabbitMQ、Keycloak、对象存储、日志或
trace 后端。迁移和 Deployment 分离，是为了让发布平台显式等待 schema 更新完成。

## 静态检查

从仓库根目录校验 Compose、迁移模板和全部 Kustomize 组合：

```bash
make validate-deploy
```

也可以只渲染生产示例：

```bash
make render-prod-k3s
```

渲染成功只说明 YAML 可以解析，不表示示例仓库、域名、Secret 或外部依赖可用于生产。

## 环境必须替换的内容

- 镜像仓库和不可变 digest；
- API 域名、TLS Secret、GatewayClass、CORS origin；
- OIDC issuer、JWKS 和服务间 token 地址；
- 三个独立数据库 DSN；
- Redis、RabbitMQ、S3、OTLP 地址和凭据；
- requests/limits、HPA、限流、熔断、超时和告警接收方。

`secret.example.yaml` 只列键名，不能直接应用。生产应由 External Secrets、Vault 或云 Secret
Manager 创建 `eagle-runtime`。product 和 order 必须使用不同的 Client Credentials，不能复用
Compose 中的 `eagle-worker`。

## 发布顺序

不要把迁移模板加回 base，也不要期望一次 `kubectl apply -k` 自动等待 Job。每个发生变化的服务
按以下顺序发布：

1. 取得该服务不可变镜像 digest；
2. 选择 `migrations/<service>.yaml`，替换为该 digest；
3. 使用 create 语义生成唯一 Job 名并等待 `Complete`；
4. 把对应 Deployment 更新到同一 digest；
5. 等待 rollout 和 `/readyz`，再进入观察窗口。

模板中的固定名称只是占位符，发布 overlay 必须追加 Git SHA 或版本号。Kubernetes Job 的
PodTemplate 不可修改；失败的迁移必须修复后创建新 Job，不能原地替换镜像或命令。

## 网关

生产入口使用 Gateway API + Envoy Gateway，不使用 Compose nginx。`base/gateway.yaml` 只创建
`Gateway` 和 `HTTPRoute`。默认的 `production-k3s` overlay 额外创建 `GatewayClass/envoy` 和
三副本 Envoy 数据面；控制器按 [K3s 生产说明](k3s/README.md) 用固定版本 Helm chart 安装。
通用 `production` overlay 仍要求平台预先提供匹配的 GatewayClass。

`gateway/` 的策略依赖 Envoy Gateway CRD，提供 TLS 版本、连接/请求超时、连接上限、限流、
least-request 和熔断。应用前确认控制器版本支持这些 API。

NetworkPolicy 只允许带 `eagle.io/gateway-access=true` 标签的命名空间访问业务 HTTP。如果
Gateway 数据面不在 `eagle` 命名空间，需要给其命名空间补该标签。Prometheus 所在命名空间同理
需要 `eagle.io/observability-access=true`。

## 内部 gRPC

product→admin 和 order→product 使用 `admin-grpc`、`product-grpc` Headless Service 与
`dns:///` 地址。Kratos 客户端通过 wrr 对就绪 Pod 做请求级均衡。不要改回普通 ClusterIP：gRPC
长连接会让负载均衡退化为按连接分配。
