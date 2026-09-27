# Eagle 微服务架构

本仓库是 Go Workspace 组织的多模块微服务单仓库，不是模块化单体。每个 `app/<service>` 都有独立 `go.mod`、Ent Client、迁移、配置和进程入口；共享源码只限技术运行时和明确的 API 契约。

## 服务边界

| 进程 | 拥有的模块 | 拥有的数据 | 依赖 |
|---|---|---|---|
| admin | access、dictionary、file、notification | 权限、字典、文件元数据、站内通知、事件 Inbox | Keycloak、S3、RabbitMQ |
| product | product | 商品 | admin 策略快照、Redis 缓存 |
| order | order | 订单、商品快照、事件 Outbox | product 查询、RabbitMQ |

Keycloak 是独立认证中心，负责用户、口令、角色和 token。本仓库不建用户表。

admin 不是业务流量网关。它组合四个基础模块，是因为这些能力当前都属于低流量管理/支撑面，没有独立扩缩容、团队或故障隔离需求。模块边界仍然保留；将来出现真实拆分信号时可以迁出，而不是现在就为每个模块维护一套镜像和数据库。

## 目录表达什么

    api/eagle/<module>/v1                         跨进程契约（独立 Go module）
    app/<service>/cmd/<service>                   进程入口和唯一组合根（Wire）
    app/<service>/internal/<module>               服务拥有的业务模块
    app/<service>/internal/platform/database/ent  服务独占的 Ent Client
    app/<service>/migrations                      服务独占的数据库迁移
    app/<service>/configs                         服务配置
    pkg                                           无业务语义的共享技术模块
    tools                                         生成器和迁移程序模块
    tests                                         架构测试与跨模块测试工具
    deploy                                        本地与生产部署资源

`app/<service>/internal` 是 Go 的编译器可见性边界：其他服务连实现包都无法 import。服务边界同时由独立 module、入口、Ent Client、数据库、API 调用和架构测试保证。根 `go.work` 只组合本地开发工作区，每个模块的依赖仍由自己的 `go.mod/go.sum` 管理。进程对象图由该服务 `cmd/<service>` 的 Wire injector 生成；`google/wire` 不能进入业务四层。

## 模块内渐进式分层

模块不按目录数量评价 DDD，而是按用例复杂度选择最小结构。纯 CRUD 或查询使用：

    service → domain ← infrastructure

存在多端口协作、聚合加载-变更-保存、事务、Outbox/Inbox、幂等、补偿、审计或多入口复用时使用：

    service → application → domain ← infrastructure

- domain：模型、不变量、领域错误和端口，只依赖标准库。
- application：可选；编排用例，只依赖本模块 domain。
- infrastructure：实现数据库、文件存储和跨服务客户端端口。
- service：入站适配，包括 protobuf Service、消息消费者和任务入口；完成协议转换并传递当前主体。

DDD 是依赖边界和不变量保护，不是代码数量指标。字典等纯 CRUD 不保留只做仓储代理的 application；订单有“商品不可用、重复商品、金额快照和总额”这些真实不变量，才使用聚合根和 application。不要为了目录对称引入工厂、领域事件或通用 DTO 体系。

## 数据所有权

本地 Docker Compose 只复用一个 PostgreSQL 实例以节省资源，但创建三个独立 database。生产可以复用数据库集群，仍必须做到：

- 每个服务使用独立 database 和账号；
- 迁移只执行 `app/<service>/migrations`；
- 禁止跨服务查询、外键和事务；
- 其他服务的数据只能通过 API 或已定义事件读取。

订单不会读取商品表，也不会把客户端报价当真。创建订单时通过 product gRPC 批量读取商品，将 SKU、名称和单价保存为订单快照，再只在订单库内提交一次事务。商品以后改名或改价不会篡改历史订单。

订单创建要求调用方提供 owner 范围内的幂等键；数据库唯一约束保证重试只得到同一订单。
同一事务会写入带独立 `event_id` 的 `event_outbox`，事件由包含 producer、schema version、
aggregate、traceparent 的统一 envelope 承载，后台 relay 经 RabbitMQ publisher confirm 发布
`OrderCreatedV1`。admin 的通知消费者先校验 envelope 和 payload，再在同一事务内写
`event_inbox` 与通知，因此重复投递不会产生重复通知。短暂消费失败做有界指数退避，
畸形消息或重试耗尽进入 DLQ，不产生无限热循环。

这条链路只演示可靠事件发布与幂等消费，不意味着库存、支付已经实现。将来出现
跨服务业务写入时，在 Outbox/Inbox 基础上增加 Saga，而不是引入跨库事务或 2PC。

## 认证与授权

    用户 token
       ↓ 每个服务本地验签
    资源服务 ──周期拉取版本化策略──> admin
       ↓                                  ↓
    本地 Casbin 判定                  策略唯一写入方

- 所有服务本地校验 JWT，admin 不成为认证代理。
- 权限要求声明在 proto 的 access / perm 上，handler 不写鉴权分支。
- admin 是策略唯一写入方；其他服务不连接权限库，只读取版本化快照并原子替换本地 Casbin 模型。
- 请求路径不做远程授权 RPC。刷新失败时保留上一份有效快照；超过三个刷新周期会使 readiness 失败，
  但不会用半份策略或空策略覆盖已有判定状态。首次启动无法取得快照则 fail closed 并拒绝启动。
- admin 的授权 gRPC 只读，不提供策略写接口；`GetPolicySnapshot`、`CheckPermission` 与
  `BatchGetProducts` 是 `INTERNAL` RPC，调用方必须携带 Keycloak
  `client_credentials` 服务令牌。

## 同步调用

只有两条同步服务依赖：

- product → admin：周期拉取授权策略快照（不在业务请求路径）；
- order → product：批量获取下单商品。

客户端在调用方业务模块的 infrastructure，application/domain 不依赖 protobuf。
调用链统一具备短期服务令牌、trace/metadata、客户端指标、熔断和有界重试；只有
两个幂等读 RPC 可以重试。开发环境使用静态地址，Compose/K3s 通过服务
DNS 解析，不嵌入注册中心 SDK。

## 部署拓扑

本地：

    Browser → nginx 开发网关:8000
                ├─ admin
                ├─ product ← order
                └─ order
                     │
            PostgreSQL（每服务独立 database）
              │ Redis / RabbitMQ / MinIO
              └──────── Keycloak

docker compose -f deploy/docker-compose.yml up -d --build 会先执行每个服务的迁移任务，再启动服务和网关。OTel、Prometheus、Tempo、Grafana 通过 --profile obs 按需启动。

生产默认使用三节点 K3s HA：

- 三个 server 节点同时运行 control-plane、embedded etcd 和工作负载，容忍一个节点故障；
- 外部 LB 访问三个节点的 Envoy Gateway；gRPC、metrics 和数据库保持集群内可达；
- 每个服务独立 Deployment、Service、HPA 和 PodDisruptionBudget；
- 迁移使用一次性 Job，成功后再滚动 Deployment；
- 配置进 ConfigMap，DSN/凭据进 Secret；
- file 多副本时把本地 BlobStore 替换为 S3/OSS/MinIO；
- NetworkPolicy 限制 order→product、资源服务→admin 和 Prometheus→metrics。

生产入口为 `deploy/kubernetes/overlays/production-k3s`，复用 `base` 中的 Deployment、Service、Gateway API、
HPA、PDB 与 NetworkPolicy。一次性迁移模板独立位于 `deploy/kubernetes/migrations`，
由发布平台等待成功后再滚动 Deployment。Redis、RabbitMQ、PostgreSQL 和 S3 在生产环境
通过 Secret 接入托管实例，不在应用清单里伪装成单副本生产集群。具体发布顺序见
[生产环境部署](deployment.md)。

## 共享代码边界

pkg 只放无业务语义、能被任意服务使用的技术原语，例如 JWT 验签、授权中间件、健康检查、配置、进程生命周期和传输运行时。业务模型不能进 pkg，服务也不能 import 其他服务拥有的模块；跨服务只 import `api` 契约。

这些约束由 tests/architecture/dependencies_test.go 持续检查。
