# 数据、Ent 与迁移

仅在修改表结构、事务、迁移、缓存或存储适配时读取。

- 每个服务独占数据库、Ent Client 和 `app/<service>/migrations`；禁止跨服务查询、外键、事务或复制对方表结构。
- 改表时同步维护服务内 `internal/platform/database/ent/schema` 和 goose SQL，再运行 `make ent`。生产只执行 goose，不使用 Ent 自动迁移。
- 迁移必须支持 CI 的 `up -> down-to 0 -> up`；除非确实不可逆且已在 SQL 注释说明，`down` 不能是空操作。
- 必须依赖锁、唯一约束或数据库当前状态的不变量，在 infrastructure 的一个事务内检查并写入，避免 application 预检造成 TOCTOU。
- infrastructure 将 Ent/SQL 的 not found、唯一冲突和并发冲突翻译成稳定的领域错误，不把 Ent 类型泄漏到上层。
- 缓存、Outbox/Inbox 和对象存储都是 infrastructure 适配；只有任务确有一致性或可靠投递需求时才引入，不作为默认 CRUD 模板。
- 可以修改 schema 和迁移，禁止手改 `internal/platform/database/ent/` 下的生成文件。
