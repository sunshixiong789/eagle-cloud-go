package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CasbinRule 是 Casbin 策略的持久化形式。
//
// 字段命名沿用 Casbin 生态的惯例（ptype + v0..v5），这样运维和排障时
// 可以直接套用社区文档与既有 SQL，不必先理解一套自造的表结构。
//
// 本项目实际只用到 ptype/v0/v1：
//   - p, <角色>, <权限码>   —— 角色被授予的权限
//   - g, <子角色>, <父角色> —— 角色继承
//
// v2..v5 预留：接入数据权限（ABAC）时会用到域、资源类型、条件表达式等维度，
// 届时只需扩展 model 而不必改表。
//
// 不用官方 casbin/ent-adapter：它自带一套 ent schema 和自动迁移，
// 会与本项目 goose 管理的迁移形成两条并行的 schema 演进路径。
type CasbinRule struct {
	ent.Schema
}

// Annotations 指定表名。
func (CasbinRule) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "casbin_rule"},
	}
}

// Fields 定义字段。
func (CasbinRule) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),

		field.String("ptype").
			MaxLen(8).
			Comment("策略类型：p=权限策略, g=角色继承"),

		field.String("v0").MaxLen(128).Default(""),
		field.String("v1").MaxLen(128).Default(""),
		field.String("v2").MaxLen(128).Default(""),
		field.String("v3").MaxLen(128).Default(""),
		field.String("v4").MaxLen(128).Default(""),
		field.String("v5").MaxLen(128).Default(""),
	}
}

// Indexes 定义索引。
func (CasbinRule) Indexes() []ent.Index {
	return []ent.Index{
		// 整行唯一，防止重复写入同一条策略造成判定结果不变但表持续膨胀
		index.Fields("ptype", "v0", "v1", "v2", "v3", "v4", "v5").Unique(),
		// 按角色过滤是最热的访问路径（RemoveFilteredPolicy / GetFilteredPolicy）
		index.Fields("ptype", "v0"),
	}
}
