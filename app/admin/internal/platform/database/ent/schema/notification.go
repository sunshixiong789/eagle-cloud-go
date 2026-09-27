package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Notification is an in-app message addressed to one Keycloak subject.
type Notification struct{ ent.Schema }

func (Notification) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "notification"}}
}

func (Notification) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("recipient_subject").MaxLen(128).Immutable(),
		field.String("sender_subject").MaxLen(128).Default("").Immutable(),
		field.String("title").MaxLen(128).Immutable(),
		field.String("content").MaxLen(4096).Immutable(),
		field.Time("read_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Notification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("recipient_subject", "created_at").
			StorageKey("idx_notification_recipient_created").
			Annotations(entsql.DescColumns("created_at")),
		index.Fields("recipient_subject", "id").
			StorageKey("idx_notification_recipient_unread").
			Annotations(entsql.IndexWhere("read_at IS NULL")),
	}
}
