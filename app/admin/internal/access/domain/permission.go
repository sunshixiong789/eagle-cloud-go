package domain

import (
	"context"
	"fmt"
	"time"
)

// PermissionType 是权限节点类型。
type PermissionType int32

const (
	PermissionTypeDir    PermissionType = 1 // 目录
	PermissionTypeMenu   PermissionType = 2 // 菜单
	PermissionTypeButton PermissionType = 3 // 按钮
)

func (t PermissionType) Valid() bool    { return t >= PermissionTypeDir && t <= PermissionTypeButton }
func (t PermissionType) IsButton() bool { return t == PermissionTypeButton }

// Status 是启用状态。
type Status int32

const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

func (s Status) Enabled() bool { return s == StatusEnabled }

// RootPermissionID 是顶级节点的 parent。用 0 而非 NULL：根是领域概念，不该由数据库可空性表达。
const RootPermissionID int64 = 0

// Permission 是权限树的聚合根。
//
// 不变量由实体自己守护：
//   - 名称非空
//   - 类型取值合法
//   - 按钮必须携带权限码
//   - 导航节点不能使用通配权限码
type Permission struct {
	id        int64
	parentID  int64
	name      string
	code      PermissionCode
	typ       PermissionType
	path      string
	component string
	icon      string
	sort      int32
	visible   bool
	status    Status
	createdAt time.Time
	updatedAt time.Time
	revision  int64
}

// NewPermissionParams 是创建/更新权限节点的入参。
type NewPermissionParams struct {
	ParentID  int64
	Name      string
	Code      string
	Type      int32
	Path      string
	Component string
	Icon      string
	Sort      int32
	Visible   bool
	Status    int32
}

// NewPermission 创建权限节点，构造即校验全部不变量。
func NewPermission(p NewPermissionParams) (*Permission, error) {
	code, err := NewPermissionCode(p.Code)
	if err != nil {
		return nil, err
	}

	perm := &Permission{
		parentID:  p.ParentID,
		name:      p.Name,
		code:      code,
		typ:       PermissionType(p.Type),
		path:      p.Path,
		component: p.Component,
		icon:      p.Icon,
		sort:      p.Sort,
		visible:   p.Visible,
		status:    Status(p.Status),
	}
	if err := perm.validate(); err != nil {
		return nil, err
	}
	return perm, nil
}

// PermissionSnapshot 是从存储读出的权限节点，仅供 infrastructure 重建实体。
type PermissionSnapshot struct {
	ID, ParentID          int64
	Name, Code            string
	Type, Status          int32
	Path, Component, Icon string
	Sort                  int32
	Visible               bool
	CreatedAt, UpdatedAt  time.Time
	Revision              int64
}

// RehydratePermission 从持久化快照重建实体，仅供 infrastructure 调用。
// 持久化数据也必须满足领域不变量；若历史数据不合法，应通过迁移修复，
// 而不是把一个无法由正常写路径创建的实体带入运行时。
func RehydratePermission(s PermissionSnapshot) (*Permission, error) {
	p := &Permission{
		id:        s.ID,
		parentID:  s.ParentID,
		name:      s.Name,
		code:      PermissionCode{value: s.Code},
		typ:       PermissionType(s.Type),
		path:      s.Path,
		component: s.Component,
		icon:      s.Icon,
		sort:      s.Sort,
		visible:   s.Visible,
		status:    Status(s.Status),
		createdAt: s.CreatedAt,
		updatedAt: s.UpdatedAt,
		revision:  s.Revision,
	}
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("rehydrate permission %d: %w", s.ID, err)
	}
	return p, nil
}

func (p *Permission) validate() error {
	if p.name == "" {
		return ErrEmptyPermissionName
	}
	if !p.typ.Valid() {
		return fmt.Errorf("%w: %d", ErrInvalidPermissionType, p.typ)
	}
	// 通配只允许出现在角色策略中。把 system:* 登记成按钮会混淆目录与授权边界。
	if p.code.HasWildcard() {
		return fmt.Errorf("%w: 导航节点只能引用具体权限码", ErrInvalidPermissionCode)
	}
	// 按钮不挂权限码就永远无法被授权，运行时表现为「有按钮但一直 403」。
	if p.typ.IsButton() && p.code.IsZero() {
		return ErrButtonRequiresCode
	}
	return nil
}

func (p *Permission) ID() int64            { return p.id }
func (p *Permission) ParentID() int64      { return p.parentID }
func (p *Permission) Name() string         { return p.name }
func (p *Permission) Code() PermissionCode { return p.code }
func (p *Permission) Type() PermissionType { return p.typ }
func (p *Permission) Path() string         { return p.path }
func (p *Permission) Component() string    { return p.component }
func (p *Permission) Icon() string         { return p.icon }
func (p *Permission) Sort() int32          { return p.sort }
func (p *Permission) Visible() bool        { return p.visible }
func (p *Permission) Status() Status       { return p.status }
func (p *Permission) CreatedAt() time.Time { return p.createdAt }
func (p *Permission) UpdatedAt() time.Time { return p.updatedAt }
func (p *Permission) Revision() int64      { return p.revision }
func (p *Permission) IsRoot() bool         { return p.parentID == RootPermissionID }
func (p *Permission) IsMenuNode() bool     { return !p.typ.IsButton() }

// Update 整体更新可变字段，任一不变量被破坏则整体不生效。
func (p *Permission) Update(params NewPermissionParams) error {
	code, err := NewPermissionCode(params.Code)
	if err != nil {
		return err
	}

	// 先在副本上应用变更，校验通过后才写回，避免失败时留下半改实体。
	next := *p
	next.parentID = params.ParentID
	next.name = params.Name
	next.code = code
	next.typ = PermissionType(params.Type)
	next.path = params.Path
	next.component = params.Component
	next.icon = params.Icon
	next.sort = params.Sort
	next.visible = params.Visible
	next.status = Status(params.Status)

	if err := next.validate(); err != nil {
		return err
	}
	*p = next
	return nil
}

// EnsureDeletable 校验删除前置条件。childCount 由调用方查得。
func (p *Permission) EnsureDeletable(childCount int64) error {
	if childCount > 0 {
		return ErrPermissionHasChildren
	}
	return nil
}

// ListPermissionsQuery 是权限列表的查询条件。
type ListPermissionsQuery struct {
	Status *Status
	Type   *PermissionType
}

// PermissionRepo 是权限聚合的仓储接口，由基础设施层实现。
type PermissionRepo interface {
	Create(ctx context.Context, p *Permission) (*Permission, error)
	GetByID(ctx context.Context, id int64) (*Permission, error)
	List(ctx context.Context, q ListPermissionsQuery) ([]*Permission, error)
	Update(ctx context.Context, p *Permission, expectedRevision *int64) (*Permission, error)
	Delete(ctx context.Context, id int64, expectedRevision *int64) error
	KnownCodes(ctx context.Context) (map[string]struct{}, error)
}
