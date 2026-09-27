package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type OutboxEvent struct{ ent.Schema }

func (OutboxEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "event_outbox"}}
}

func (OutboxEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(36).Immutable(),
		field.String("aggregate_id").MaxLen(128).Immutable(),
		field.String("event_type").MaxLen(128).Immutable(),
		field.String("routing_key").MaxLen(128).Immutable(),
		field.Bytes("payload").Immutable(),
		field.Int32("attempts").Default(0).NonNegative(),
		field.String("last_error").MaxLen(1024).Default(""),
		field.Time("available_at").Default(time.Now),
		field.Time("locked_until").Optional().Nillable(),
		field.Time("published_at").Optional().Nillable(),
		field.Time("failed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (OutboxEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("published_at", "available_at", "created_at").StorageKey("idx_event_outbox_pending"),
		index.Fields("failed_at", "created_at").StorageKey("idx_event_outbox_failed"),
	}
}
