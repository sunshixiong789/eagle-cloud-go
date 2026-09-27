package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// File stores metadata for a blob owned by one authenticated subject.
type File struct{ ent.Schema }

func (File) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "stored_file"}}
}

func (File) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(36).Immutable(),
		field.String("owner_subject").MaxLen(128).Immutable(),
		field.String("name").MaxLen(255).Immutable(),
		field.String("storage_key").MaxLen(255).Unique().Immutable(),
		field.String("content_type").MaxLen(128).Default("application/octet-stream").Immutable(),
		field.Int64("size").NonNegative().Immutable(),
		field.String("sha256").MaxLen(64).Immutable(),
		field.String("state").MaxLen(16).Default("pending"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (File) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_subject", "created_at").
			StorageKey("idx_stored_file_owner_created").
			Annotations(entsql.DescColumns("created_at")),
		index.Fields("state", "updated_at").StorageKey("idx_stored_file_state_updated"),
	}
}
