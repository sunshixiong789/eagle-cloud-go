# Go 代码约定

仅补充本项目特有约束，不复述 Effective Go。

- 标准库优先，尤其使用 `slices`、`maps`、`cmp`、`errors`、`fmt.Errorf("%w")` 和 `log/slog`。架构测试列出的禁用依赖不得新增。
- 映射逻辑写成所在边界的小函数，例如 `toProtoXxx` / `toDomainXxx`，不引入通用 mapper、反射复制或 `XxxDTO` / `XxxDO` 体系。
- domain/application 使用标准库错误；框架错误只存在于 service/server 边界。判断错误使用 `errors.Is` / `errors.As`。
- 值对象只用于能显著防止非法状态或静默错误的业务概念；参数多且同类型易传错时使用明确的 `XxxParams`。
- 包名表达业务或技术职责，不使用 `utils`、`common`、`helpers`、`models` 作为兜底包。
- 注释解释不明显的业务约束或设计原因，不复述代码，不添加占位 TODO。
- 日志统一使用 `log/slog`；Wire 仅用于服务组合根；生成文件通过 Make 目标更新，不直接编辑。
