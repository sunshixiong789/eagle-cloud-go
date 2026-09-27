package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Permission 是前端导航节点。类型名为兼容现有 API 和领域模型而保留，
// 持久化表已经与后端 permission_definition 分离。
//
// 权限码（code）是三方契约的交汇点：proto 注解上写
// (eagle.annotations.v1.perm) = "system:user:add"，Casbin 策略里是同一个
// 字符串，本表存它的元数据。三处对不上就是全线 403。
//
// 刻意不为 parent_id 声明 ent edge：根节点用 parent_id=0 表示「无父级」，
// 而 edge 会生成真实外键约束，0 指向不存在的行会直接插入失败。
// 树结构在应用层按 parent_id 拼装，权限总量只有百级，成本可忽略。
type Permission struct {
	ent.Schema
}

// Annotations 指定表名。
func (Permission) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "navigation_node"},
	}
}

// Fields 定义字段。
//
// type / status 用数值而非 ent enum：proto 契约里是 int32，
// 字典表种入的也是 "1"/"2"/"3"，换成字符串枚举会让三处表示不一致。
func (Permission) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),

		field.Int64("parent_id").
			Optional().
			Nillable().
			Comment("顶级节点在数据库中为 NULL，领域层映射为 0"),

		field.String("name").
			MaxLen(64).
			NotEmpty(),

		field.String("code").
			MaxLen(128).
			Optional().
			Nillable().
			StorageKey("permission_code").
			Comment("权限码，如 system:user:add。目录/菜单可为空，按钮必填"),

		field.Int32("type").
			Comment("1=目录 2=菜单 3=按钮"),

		field.String("path").MaxLen(255).Default(""),
		field.String("component").MaxLen(255).Default(""),
		field.String("icon").MaxLen(64).Default(""),

		field.Int32("sort").Default(0),
		field.Bool("visible").Default(true),

		field.Int32("status").
			Default(1).
			Comment("0=禁用 1=正常"),

		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes 定义索引。
func (Permission) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code").Unique(),
		index.Fields("parent_id"),
	}
}
