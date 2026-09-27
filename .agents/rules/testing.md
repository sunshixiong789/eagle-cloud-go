# 测试与验证

测试应覆盖改动带来的风险，而不是机械追求每层都有测试。

| 改动 | 主要验证 |
|---|---|
| domain 规则或聚合 | 表驱动单测覆盖合法路径、边界值和不变量 |
| application 编排 | 用端口 fake 验证调用顺序、错误传播和结果 |
| infrastructure | 使用真实 PostgreSQL 或临时存储验证查询、事务、并发与错误翻译 |
| service / API | 验证 Proto 映射、错误映射、401 / 403 / 200 和内部服务身份 |
| 分层、依赖或新服务 | `go test ./tests/architecture/...` |
| schema / migration | 独立服务迁移目录执行 `up -> down -> up` |

- 单元和集成测试不依赖 Docker；现有数据库集成测试使用 embedded-postgres。
- e2e 认证链路必须走真实验签，不向 context 塞假 Principal 绕过中间件。
- 修改公共 matcher、权限覆盖规则或跨层约束时，同时运行其一致性/架构测试。
- 先运行最小相关包测试；跨模块或生成类改动再运行 `make lint`、`make test`。保留实际命令输出作为“已验证”的依据。
