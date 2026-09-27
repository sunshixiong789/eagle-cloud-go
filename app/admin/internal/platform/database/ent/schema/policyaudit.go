package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PolicyAudit 是只追加的授权变更审计记录。
type PolicyAudit struct{ ent.Schema }

func (PolicyAudit) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "authz_policy_audit"}}
}

func (PolicyAudit) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("policy_version").NonNegative(),
		field.String("action").MaxLen(64),
		field.String("target").MaxLen(256),
		field.String("actor_subject").MaxLen(128).Default(""),
		field.String("actor_client_id").MaxLen(128).Default(""),
		field.String("request_id").MaxLen(128).Default(""),
		field.String("trace_id").MaxLen(64).Default(""),
		field.JSON("before", []string{}),
		field.JSON("after", []string{}),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (PolicyAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("policy_version"),
		index.Fields("target", "created_at"),
		index.Fields("actor_subject", "created_at"),
	}
}
