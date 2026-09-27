# eagle-go 项目规则

本文件是编码 Agent 每次进入仓库都要遵守的项目级约束，只保留长期稳定、会影响代码结构和正确性的内容。产品说明、操作教程和部署步骤分别放在 `README.md` 与 `docs/`，不要复制到这里。

## 开始修改前

- 先确认改动属于哪个服务、哪个业务模块，并阅读相邻实现；只修改完成任务必需的文件。
- 优先沿用仓库已有模式：简单 CRUD 参考 `dictionary`，有业务不变量的模型参考 `order` 或 `access`。
- 不从 Java / Spring 搬运 controller-service-repository、starter、统一 DTO/DO、通用 mapper 等体系，也不为未来需求预埋接口、表或抽象。
- 工作区可能已有用户改动；不要覆盖、回滚或顺手整理无关文件。

## 架构与 DDD

本仓库是 Go Workspace 组织的多模块微服务单仓库。代码先按服务划分，再按业务模块划分：

```text
app/<service>/
├── cmd/<service>               # 进程入口和 Wire 组合根
├── internal/<module>/
│   ├── service                 # 入站适配：Protobuf、消息消费和任务入口
│   ├── application             # 可选；只在存在用例编排时创建
│   ├── domain                  # 领域模型、规则、错误和端口
│   └── infrastructure          # 出站适配：数据库、缓存、消息发布和远程调用
├── internal/platform/database  # 当前服务独占的数据库与 Ent Client
└── migrations                  # 当前服务独占的迁移
```

模块按复杂度渐进生长，允许两种依赖形态：

```text
service -> domain <- infrastructure

或

service -> application -> domain <- infrastructure
```

- `domain` 只依赖标准库，负责业务概念、纯业务不变量、领域错误，以及被用例实际需要的仓储或外部能力端口。
- `application` 是可选的，只依赖本模块 `domain`，负责一个用例的流程编排；不依赖 Proto、Kratos、Ent、Casbin 或具体客户端。
- `infrastructure` 实现 domain 端口，负责 Ent/SQL、事务、缓存、消息、文件存储和跨服务客户端，并把技术错误翻译为领域错误。
- `service` 承载入站适配，只做协议对象转换、主体传递和错误边界适配；不写业务规则、事务、补偿或持久化逻辑，也不 import `infrastructure`。

DDD 用来保护边界和不变量，不用来增加代码量：

- 只有存在生命周期、状态转换或必须始终成立的业务规则时，才使用聚合根、值对象或领域方法。字典、查询等简单 CRUD 保持简单。
- 仅包含 `return repo.Xxx(...)` 的 application 应删除，由 service 依赖 domain 定义的最小端口。一旦用例需要多端口协作、聚合加载-变更-保存、事务、Outbox/Inbox、幂等、补偿、审计或多入口复用，必须增加 application。
- 不依赖 I/O 的规则放在 domain 构造器或方法中；跨端口的用例流程放在 application；必须依赖数据库锁、唯一约束或当前持久化状态的检查，放在 infrastructure 的同一事务内，不在 application 重复预检。
- 一个聚合的原子持久化可由聚合仓储内部完成；涉及多个本地写入时使用语义化的原子端口，禁止向 application 暴露 Ent Tx 或通过 context 隐式传递事务。
- 更新端口优先接收只含允许修改字段的明确参数，不用完整公开实体依赖调用方约定保护不可变字段。
- domain 定义的是业务需要的最小端口，不是对 Ent API 的包装。端口方法必须有当前生产调用方。
- 不为了“完整 DDD”强行加入工厂、领域事件、通用 BaseRepository、DTO/DO 或多余分层。

## 服务与数据边界

- 每个 `app/<service>` 独立拥有 `go.mod`、入口、配置、Ent Client、迁移和数据库。组合根只能装配当前服务拥有的模块，Wire 只允许出现在 `cmd/<service>`。
- 服务之间禁止 import 对方 `app/` 实现、查询对方数据库、建立跨服务外键或事务。同步调用只依赖 `api/` 契约，异步协作使用已定义事件。
- 跨服务客户端放在调用方模块的 `infrastructure`，由它实现本模块 domain 端口；domain/application 不接触生成的 Protobuf client。
- `pkg/` 只放无业务语义、可被多个服务复用的技术能力，禁止 import `app/`。业务模型留在拥有它的模块。

## API、身份与数据

- API 先改 `api/**/*.proto`。每个 RPC 必须显式声明 `access`；需要权限时同时声明 `perm`。鉴权由中间件完成，handler 不写重复鉴权分支。
- Keycloak 负责用户和角色，本仓库不建用户表。当前主体统一从 `pkg/identity` 获取，不自行解析 JWT 或创建第二套 Principal。
- 表结构通过服务内 Ent schema 表达，生产迁移通过同一服务的 goose SQL 表达；禁止跨服务共享 Ent Client 或迁移目录。
- 只修改源文件。禁止手改 `*.pb.go`、`internal/platform/database/ent/`、`wire_gen.go` 等生成文件；分别通过 `make api`、`make ent`、`make wire` 或 `make generate` 生成。

## 质量底线

- Go 标准库优先，新增生产依赖前先确认现有依赖或 `pkg/` 不能解决；不要引入通用复制、转换、集合辅助库替代几行明确代码。
- 测试跟随风险：domain 测不变量，application 测用例编排，infrastructure 测真实适配和事务，service/e2e 测协议、身份与授权边界。
- 完成后至少对修改过的 Go 文件执行格式化并运行相关包测试。涉及跨层依赖、生成代码、迁移或服务装配时，再运行对应生成命令、架构测试或 `make lint && make test`。
- 未实际运行的命令不要声称通过；如果受环境限制无法验证，明确说明未验证项。

## 按需阅读

不要在每个任务开始时通读所有文档，只按改动范围读取：

| 任务 | 先读 |
|---|---|
| 新增或修改 API | `README.md` 的“新增或修改 API” + `.agents/rules/api-authz.md` |
| 新增模块或 CRUD | `README.md` 的“新增业务模块或 CRUD” |
| 判断服务边界或跨服务协作 | `docs/architecture.md` |
| 修改 schema、迁移或 Ent | `.agents/rules/data.md` |
| 新写 Go 或引入依赖 | `.agents/rules/style.md` |
| 设计或补充测试 | `.agents/rules/testing.md` |
