package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// PermissionTreeState 是权限树的全局乐观并发版本与事务串行化锁。
type PermissionTreeState struct{ ent.Schema }

func (PermissionTreeState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "permission_tree_state"}}
}

func (PermissionTreeState) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("revision").Default(1).Positive(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
