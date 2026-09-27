package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type OrderItem struct{ ent.Schema }

func (OrderItem) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "order_item"}}
}

func (OrderItem) Fields() []ent.Field {
	return []ent.Field{
		field.String("order_id").MaxLen(36),
		field.Int64("product_id").Positive(),
		field.String("product_sku").MaxLen(64),
		field.String("product_name").MaxLen(128),
		field.Int64("unit_price_cents").Positive(),
		field.Int32("quantity").Positive(),
		field.Int64("subtotal_cents").Positive(),
	}
}

func (OrderItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("order_id").StorageKey("idx_order_item_order"),
		index.Fields("order_id", "product_id").Unique(),
	}
}
