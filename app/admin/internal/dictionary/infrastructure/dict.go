package infrastructure

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/admin/internal/dictionary/domain"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/dictdata"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/dicttype"
)

type dictRepo struct {
	db *platformdb.Database
}

// NewDictRepo 构造字典仓储。
func NewDictRepo(db *platformdb.Database) domain.DictRepo {
	return &dictRepo{db: db}
}

func toDomainDictType(t *ent.DictType) *domain.DictType {
	if t == nil {
		return nil
	}
	return &domain.DictType{
		ID:        t.ID,
		Name:      t.Name,
		Type:      t.Type,
		Status:    domain.Status(t.Status),
		Remark:    t.Remark,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func toDomainDictData(d *ent.DictData) *domain.DictData {
	if d == nil {
		return nil
	}
	return &domain.DictData{
		ID:        d.ID,
		DictType:  d.DictType,
		Label:     d.Label,
		Value:     d.Value,
		Sort:      d.Sort,
		CSSClass:  d.CSSClass,
		IsDefault: d.IsDefault,
		Status:    domain.Status(d.Status),
		Remark:    d.Remark,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// ── 字典类型 ──────────────────────────────────────────────

func (r *dictRepo) CreateType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	created, err := r.db.Client().DictType.Create().
		SetName(t.Name).
		SetType(t.Type).
		SetStatus(int32(t.Status)).
		SetRemark(t.Remark).
		Save(ctx)
	if err != nil {
		if platformdb.IsUniqueViolation(err) {
			return nil, domain.ErrDictTypeDuplicated
		}
		return nil, fmt.Errorf("create dict type: %w", err)
	}
	return toDomainDictType(created), nil
}

func (r *dictRepo) GetTypeByID(ctx context.Context, id int64) (*domain.DictType, error) {
	t, err := r.db.Client().DictType.Get(ctx, id)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("get dict type %d: %w", id, err)
	}
	return toDomainDictType(t), nil
}

func (r *dictRepo) ListTypes(ctx context.Context, q domain.ListDictTypesQuery) ([]*domain.DictType, int64, error) {
	query := r.db.Client().DictType.Query()
	if q.Keyword != "" {
		query = query.Where(dicttype.Or(
			dicttype.NameContainsFold(q.Keyword),
			dicttype.TypeContainsFold(q.Keyword),
		))
	}
	if q.Status != nil {
		query = query.Where(dicttype.StatusEQ(int32(*q.Status)))
	}

	// 先数总数再取当页：两次查询共用同一组谓词，避免翻页时
	// 总数与列表来自不同的过滤条件
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count dict types: %w", err)
	}

	rows, err := query.
		Order(ent.Asc(dicttype.FieldID)).
		Offset(int(q.Offset)).
		Limit(int(q.PageSize)).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list dict types: %w", err)
	}

	out := make([]*domain.DictType, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainDictType(row))
	}
	return out, int64(total), nil
}

func (r *dictRepo) UpdateType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	updated, err := r.db.Client().DictType.UpdateOneID(t.ID).
		SetName(t.Name).
		SetStatus(int32(t.Status)).
		SetRemark(t.Remark).
		Save(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("update dict type %d: %w", t.ID, err)
	}
	return toDomainDictType(updated), nil
}

func (r *dictRepo) DeleteType(ctx context.Context, id int64) error {
	if err := r.db.Client().DictType.DeleteOneID(id).Exec(ctx); err != nil {
		if platformdb.IsNotFound(err) {
			return domain.ErrDictTypeNotFound
		}
		return fmt.Errorf("delete dict type %d: %w", id, err)
	}
	return nil
}

// ── 字典项 ────────────────────────────────────────────────

func (r *dictRepo) CreateData(ctx context.Context, d *domain.DictData) (*domain.DictData, error) {
	created, err := r.db.Client().DictData.Create().
		SetDictType(d.DictType).
		SetLabel(d.Label).
		SetValue(d.Value).
		SetSort(d.Sort).
		SetCSSClass(d.CSSClass).
		SetIsDefault(d.IsDefault).
		SetStatus(int32(d.Status)).
		SetRemark(d.Remark).
		Save(ctx)
	if err != nil {
		if platformdb.IsUniqueViolation(err) {
			return nil, domain.ErrDictDataDuplicated
		}
		// dict_type 有外键指向 sys_dict_type.type，
		// 挂到不存在的类型下会触发外键冲突
		if platformdb.IsForeignKeyViolation(err) {
			return nil, domain.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("create dict data: %w", err)
	}
	return toDomainDictData(created), nil
}

func (r *dictRepo) GetDataByID(ctx context.Context, id int64) (*domain.DictData, error) {
	d, err := r.db.Client().DictData.Get(ctx, id)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrDictDataNotFound
		}
		return nil, fmt.Errorf("get dict data %d: %w", id, err)
	}
	return toDomainDictData(d), nil
}

func (r *dictRepo) ListData(ctx context.Context, q domain.ListDictDataQuery) ([]*domain.DictData, int64, error) {
	query := r.db.Client().DictData.Query()
	if q.DictType != nil {
		query = query.Where(dictdata.DictTypeEQ(*q.DictType))
	}
	if q.Keyword != "" {
		query = query.Where(dictdata.LabelContainsFold(q.Keyword))
	}
	if q.Status != nil {
		query = query.Where(dictdata.StatusEQ(int32(*q.Status)))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count dict data: %w", err)
	}

	rows, err := query.
		Order(ent.Asc(dictdata.FieldDictType), ent.Asc(dictdata.FieldSort), ent.Asc(dictdata.FieldID)).
		Offset(int(q.Offset)).
		Limit(int(q.PageSize)).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list dict data: %w", err)
	}

	out := make([]*domain.DictData, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainDictData(row))
	}
	return out, int64(total), nil
}

func (r *dictRepo) ListDataByType(ctx context.Context, dictType string) ([]*domain.DictData, error) {
	rows, err := r.db.Client().DictData.Query().
		Where(
			dictdata.DictTypeEQ(dictType),
			dictdata.StatusEQ(int32(domain.StatusEnabled)),
		).
		Order(ent.Asc(dictdata.FieldSort), ent.Asc(dictdata.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dict data of type %q: %w", dictType, err)
	}
	out := make([]*domain.DictData, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainDictData(row))
	}
	return out, nil
}

func (r *dictRepo) UpdateData(ctx context.Context, update domain.UpdateDictData) (*domain.DictData, error) {
	updated, err := r.db.Client().DictData.UpdateOneID(update.ID).
		SetLabel(update.Label).
		SetValue(update.Value).
		SetSort(update.Sort).
		SetCSSClass(update.CSSClass).
		SetIsDefault(update.IsDefault).
		SetStatus(int32(update.Status)).
		SetRemark(update.Remark).
		Save(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrDictDataNotFound
		}
		if platformdb.IsUniqueViolation(err) {
			return nil, domain.ErrDictDataDuplicated
		}
		return nil, fmt.Errorf("update dict data %d: %w", update.ID, err)
	}
	return toDomainDictData(updated), nil
}

func (r *dictRepo) DeleteData(ctx context.Context, id int64) error {
	if err := r.db.Client().DictData.DeleteOneID(id).Exec(ctx); err != nil {
		if platformdb.IsNotFound(err) {
			return domain.ErrDictDataNotFound
		}
		return fmt.Errorf("delete dict data %d: %w", id, err)
	}
	return nil
}
