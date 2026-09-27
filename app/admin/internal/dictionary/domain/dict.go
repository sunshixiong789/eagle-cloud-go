package domain

import (
	"context"
	"time"
)

// Status belongs to this module; sharing the access module's identically
// represented status would create a false business dependency.
type Status int32

const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

func (s Status) Enabled() bool { return s == StatusEnabled }

// DictType 是字典类型实体。
//
// 字典是纯 CRUD，没有需要守护的业务不变量，因此刻意保持贫血：
// 为它套聚合根、值对象、领域事件只会增加仪式感而不产生任何收益。
// 战术 DDD 应当用在有真实不变量的地方（权限树、角色绑定），
// 而不是无差别地铺满每个模块。
type DictType struct {
	ID        int64
	Name      string
	Type      string
	Status    Status
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DictData 是字典项实体。
type DictData struct {
	ID        int64
	DictType  string
	Label     string
	Value     string
	Sort      int32
	CSSClass  string
	IsDefault bool
	Status    Status
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListDictTypesQuery 是字典类型的查询条件。
type ListDictTypesQuery struct {
	Keyword  string
	Status   *Status
	Offset   int64
	PageSize int32
}

// ListDictDataQuery 是字典项的查询条件。
type ListDictDataQuery struct {
	DictType *string
	Keyword  string
	Status   *Status
	Offset   int64
	PageSize int32
}

// UpdateDictData 只暴露字典项允许修改的字段。
// DictType 创建后不可变，因此不出现在更新参数中。
type UpdateDictData struct {
	ID        int64
	Label     string
	Value     string
	Sort      int32
	CSSClass  string
	IsDefault bool
	Status    Status
	Remark    string
}

// DictRepo 是字典的仓储接口，由基础设施层实现。
type DictRepo interface {
	CreateType(ctx context.Context, t *DictType) (*DictType, error)
	GetTypeByID(ctx context.Context, id int64) (*DictType, error)
	ListTypes(ctx context.Context, q ListDictTypesQuery) ([]*DictType, int64, error)
	UpdateType(ctx context.Context, t *DictType) (*DictType, error)
	DeleteType(ctx context.Context, id int64) error

	CreateData(ctx context.Context, d *DictData) (*DictData, error)
	GetDataByID(ctx context.Context, id int64) (*DictData, error)
	ListData(ctx context.Context, q ListDictDataQuery) ([]*DictData, int64, error)
	// ListDataByType 是前端下拉框的主要来源。
	ListDataByType(ctx context.Context, dictType string) ([]*DictData, error)
	UpdateData(ctx context.Context, update UpdateDictData) (*DictData, error)
	DeleteData(ctx context.Context, id int64) error
}
