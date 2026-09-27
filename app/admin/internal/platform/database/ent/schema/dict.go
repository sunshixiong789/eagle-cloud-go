package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DictType 是字典类型，一组字典项的容器。
type DictType struct {
	ent.Schema
}

// Annotations 指定表名。
func (DictType) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_dict_type"},
	}
}

// Fields 定义字段。
func (DictType) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),

		field.String("name").MaxLen(64).NotEmpty(),

		field.String("type").
			MaxLen(64).
			Unique().
			Comment("字典类型标识，如 sys_common_status。被字典项引用，创建后不可改"),

		field.Int32("status").Default(1),
		field.String("remark").Default(""),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// DictData 是字典项。
//
// 通过 dict_type 字符串关联 DictType 而不是用 ent edge：
// 前端和业务代码都按类型名（sys_common_status）取值，
// 用字符串关联可以避免每次查询都要先解析出类型 ID。
type DictData struct {
	ent.Schema
}

// Annotations 指定表名。
func (DictData) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_dict_data"},
	}
}

// Fields 定义字段。
func (DictData) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),

		field.String("dict_type").
			MaxLen(64).
			Comment("关联 sys_dict_type.type"),

		field.String("label").MaxLen(128).NotEmpty(),
		field.String("value").MaxLen(128).NotEmpty(),

		field.Int32("sort").Default(0),
		field.String("css_class").MaxLen(64).Default(""),
		field.Bool("is_default").Default(false),
		field.Int32("status").Default(1),
		field.String("remark").Default(""),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes 定义索引。
func (DictData) Indexes() []ent.Index {
	return []ent.Index{
		// 同一字典下键值唯一
		index.Fields("dict_type", "value").Unique(),
		// 按类型取值是最热的查询路径（前端下拉框）
		index.Fields("dict_type", "sort"),
	}
}
