package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PermissionDefinition 是后端授权契约目录，与前端导航生命周期解耦。
type PermissionDefinition struct{ ent.Schema }

func (PermissionDefinition) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "permission_definition"}}
}

func (PermissionDefinition) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("code").MaxLen(128).Unique(),
		field.String("service").MaxLen(64),
		field.String("resource").MaxLen(64),
		field.String("action").MaxLen(64),
		field.Int32("status").Default(1),
		field.String("source").MaxLen(32).Default("manual"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PermissionDefinition) Indexes() []ent.Index {
	return []ent.Index{index.Fields("service", "resource", "action")}
}
