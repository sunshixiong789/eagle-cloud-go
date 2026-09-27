package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Product struct{ ent.Schema }

func (Product) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("sku").MaxLen(64).Unique(),
		field.String("name").MaxLen(128),
		field.String("description").MaxLen(2048).Default(""),
		field.Int64("price_cents").Positive(),
		field.Bool("active").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Product) Indexes() []ent.Index {
	return []ent.Index{index.Fields("active", "created_at").StorageKey("idx_products_active_created")}
}
