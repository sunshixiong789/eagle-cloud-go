# 平台落地约定

## 定位与边界

当前底座面向单企业内部管理与业务服务：OIDC 身份、接口 RBAC、主体所有权、商品与订单示例。
它没有组织树、多租户数据隔离、审批、支付和库存模型；这些能力应由真实产品需求驱动设计，
不能把接口 RBAC 或 Keycloak realm 自动等同于租户隔离。

admin 保持支撑模块组合，product/order 演示独立业务服务。出现独立团队、容量或故障隔离需求时再拆服务。
部署可接入企业现有 Kubernetes 平台；K3s 是仓库提供的部署方案，不要求企业另建第二套平台。

## 新服务的最小接入路径

不要复制整个 admin。纯 CRUD 参考 dictionary；有编排和不变量的业务参考 order。
只创建当前用例需要的层，入口和生命周期参考 product 的 cmd 组合根：

1. 明确拥有的数据、RPC、调用方和负责人；为服务建立独立 go.mod、configs、database 和 migrations。
2. api 定义 access/perm，权限目录通过 admin 迁移声明；只引入该服务需要的入站接口。
3. domain 定义最小端口，infrastructure 实现端口，cmd 组合依赖。
4. 加入 go.work、Makefile SERVICES/MODULES、Dockerfile 服务校验分支和 CI 镜像矩阵。
5. 根据实际需要配置 Keycloak audience、调用方独立 client、Gateway 路由、NetworkPolicy、Secret、迁移 Job、探针和观测标签。
6. 运行 make generate、make lint、make test、make build-independent、make validate-deploy，并验证自身镜像构建。
7. 在隔离环境完整走一次新接口的 401/403/成功、迁移与滚动发布。已有契约消费者必须仍能使用旧版本。

当前用这些受测参考实现和明确接入步骤提供模板，不使用批量代码生成器预埋业务表、端口或层。

## 共享依赖与服务装配

api/pkg 仍使用单仓库本地 replace，意味着源码升级需要评估所有消费者。make build-independent
关闭 Workspace 编译每个模块，防止 go.work 掩盖依赖缺失；它不证明服务间语义兼容。
新增 RPC 使用兼容字段，移除字段先 reserved，破坏性调整升 API 版本，保留消费者迁移窗口。
发布共享代码变更时运行所有服务测试和镜像构建，滚动窗口验证 N 与 N-1 版本组合。

三个服务在各自 `cmd/<service>/app.go` 中手写组合根，直接调用构造器，不使用 DI 生成器或运行时容器。
新增模块时同步更新 `buildApp` 和 `server.go` 的协议注册。初始化失败必须释放已创建资源；后台任务在
所需依赖就绪后启动，正常退出时先停止任务，再按依赖逆序关闭客户端和数据库。组合根只装配本服务模块。
调整装配或升级 Go 工具链后，运行 `make generate`、`make lint`、`make test` 和 `make build-independent`。

## 交付门禁和责任

CI 提供生成差异、API 兼容性、lint、race、架构、PostgreSQL 迁移、真实 Redis/RabbitMQ 测试，
以及独立模块与镜像构建。Release 工作流生成 SBOM/provenance，按架构扫描候选镜像后发布 digest。
需由仓库管理员启用必需状态检查与 release environment，环境仓库负责部署授权、镜像签名校验和环境 Secret。
这些平台配置必须以实际设置验收，不能把 workflow 文件等同于已启用的保护规则。

团队应为以下事项指定实际负责人，不在公共模板中虚构组织账号：业务服务及 API、共享 api/pkg、
身份权限、数据库迁移与恢复、运行平台和值班。依赖升级由 Dependabot 提出变更，经同一门禁后合入。

## 本地故障回归

普通 make test 使用 embedded-postgres，Redis/RabbitMQ 适配测试需要显式测试地址。
这些地址必须指向测试环境，不连接生产。make test-adapters 在地址缺失时直接失败。

```bash
EAGLE_TEST_REDIS_ADDRESS=127.0.0.1:6379 EAGLE_TEST_RABBITMQ_URL=amqp://eagle:eagle@127.0.0.1:5672/ make test-adapters
```

真实适配测试覆盖：无绑定时不得完成 Outbox；建立绑定后重试成功；发布断线后恢复；
消费停机重入队；并发同键订单只生成一个订单/事件；不同请求冲突；旧订单摘要兼容；
缓存旧令牌在失效后不能回填；Redis 不可用时数据库降级；Redis TLS CA/主机名验证；
quorum 源队列在 DLQ 缺失绑定时保留消息，绑定恢复后继续投递。
`app/order/tests/e2e` 还启动三个真实服务进程和各自独立的 PostgreSQL 数据库，验证 HTTP 下单、
服务间凭据与 gRPC 调用、Outbox 缺失绑定重试、重启后的订单幂等和 Inbox 去重。
测试让 S3 不可达以验证 admin 和授权链仍可启动；只有 Keycloak 签发端由测试 RS256/JWKS 服务替代，
服务内部验签、audience 和授权中间件均照常运行。它不替代真实 Keycloak 配置和多节点 broker 故障演练。
本地 TLS 测试使用证书校验代理连接真实 Redis，生产还需验证供应商端点及证书轮换。

## 生产环境验收记录

下面是实际环境的发布前验收项，不是已经完成的演练报告。记录时间、版本 digest、执行人、观测链接和结果：

| 场景 | 通过条件 |
|---|---|
| admin 策略服务中断 | 普通权限请求在配置有效期后拒绝；受信任的内部商品查询和已有订单查询仍可用；恢复后自动恢复授权 |
| Redis 故障、重启和恢复 | product 能启动并回退数据库；缓存预算不吞掉整个请求超时；数据库峰值连接和负载可接受 |
| RabbitMQ 中断、缺失绑定、恢复 | 订单与 Outbox 原子提交；未路由消息保持待处理；恢复后通知不重复；DLQ/Outbox 告警送达 |
| 服务滚动重启 | 请求不产生重复业务写入；处理中消息重入队；旧版本可读新 schema |
| TLS 与凭据轮换 | 私有 CA、主机名和服务身份有效；错误证书拒绝；凭据滚动切换成功 |
| 数据恢复 | 恢复到隔离库，验证迁移、关键读接口与数量；记录实测 RPO/RTO |
| 压测与扩容 | 记录吞吐、p99、错误率和资源；最大副本数 × 单实例连接上限，加迁移/运维余量不超过数据库预算 |
| 告警演练 | 触发、路由到值班人员、确认和恢复全链路有证据 |

SLO、RPO/RTO 和资源阈值是待容量与恢复测试验证的目标，不能只凭清单渲染成功宣称达到。
