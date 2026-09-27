package domain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// Role 是带来源命名空间的 Keycloak 角色键。
type Role string

func NewRole(s string) (Role, error) {
	valid := false
	switch {
	case strings.HasPrefix(s, "realm:"):
		name := strings.TrimPrefix(s, "realm:")
		valid = name != "" && !strings.Contains(name, ":")
	case strings.HasPrefix(s, "client:"):
		parts := strings.Split(strings.TrimPrefix(s, "client:"), ":")
		valid = len(parts) == 2 && parts[0] != "" && parts[1] != ""
	}
	if !valid {
		return "", ErrEmptyRole
	}
	return Role(s), nil
}

func (r Role) String() string { return string(r) }
func (r Role) IsZero() bool   { return r == "" }

// RoleBinding 是「角色 → 权限码集合」的聚合根，对应 Casbin 的 p 策略。
//
// 聚合边界刻意不包含用户：用户到角色的归属由 Keycloak 维护，
// 把它纳进来就等于与 Keycloak 双写同一份数据，必然漂移。
type RoleBinding struct {
	role     Role
	codes    []PermissionCode
	revision int64
}

// NewRoleBinding 构造角色权限绑定，并对权限码去重。
//
// 去重而不是报错：重复授予同一权限码不改变判定结果，
// 为它报错只会让调用方在无害的输入上受阻。
func NewRoleBinding(role Role, codes []PermissionCode) (*RoleBinding, error) {
	if role.IsZero() {
		return nil, ErrEmptyRole
	}

	seen := make(map[string]struct{}, len(codes))
	deduped := make([]PermissionCode, 0, len(codes))
	for _, c := range codes {
		if c.IsZero() {
			continue
		}
		if _, dup := seen[c.String()]; dup {
			continue
		}
		seen[c.String()] = struct{}{}
		deduped = append(deduped, c)
	}

	slices.SortFunc(deduped, func(a, b PermissionCode) int {
		return cmp.Compare(a.String(), b.String())
	})

	return &RoleBinding{role: role, codes: deduped}, nil
}

func (b *RoleBinding) Role() Role              { return b.role }
func (b *RoleBinding) Codes() []PermissionCode { return b.codes }
func (b *RoleBinding) CodeStrings() []string   { return PermissionCodeStrings(b.codes) }
func (b *RoleBinding) IsEmpty() bool           { return len(b.codes) == 0 }
func (b *RoleBinding) Revision() int64         { return b.revision }

// WithRevision 返回带权威策略版本的副本。
func (b *RoleBinding) WithRevision(revision int64) *RoleBinding {
	if b == nil {
		return nil
	}
	next := *b
	next.revision = revision
	return &next
}

// Grants 判断本绑定是否覆盖目标权限码（考虑通配）。
//
// 与 Casbin 模型里的 keyMatch 语义保持一致。领域层保留一份独立实现，
// 是为了让「授权是否生效」可以脱离 Casbin 单测；
// 两处一旦不同步，就会出现后台显示已授权、实际调用却 403。
func (b *RoleBinding) Grants(target PermissionCode) bool {
	return slices.ContainsFunc(b.codes, func(c PermissionCode) bool {
		return c.Covers(target)
	})
}

// EnsureCodesKnown 校验全部具体权限码都存在于权限目录中。
//
// 含通配的策略跳过校验——它本就不对应目录里的一条契约。
// 不做这道校验的话，拼错的权限码会静默失效：策略写进去了，
// 却永远匹配不到任何接口，而配置的人以为已经授权成功。
func (b *RoleBinding) EnsureCodesKnown(known map[string]struct{}) error {
	for _, c := range b.codes {
		if c.HasWildcard() {
			continue
		}
		if _, ok := known[c.String()]; !ok {
			return fmt.Errorf("%w: %s", ErrUnknownPermissionCode, c)
		}
	}
	return nil
}

// RoleInheritance 是角色继承关系，对应 Casbin 的 g 规则。
type RoleInheritance struct {
	Child  Role
	Parent Role
}

// NewRoleInheritance 构造继承关系并拦截自继承。
func NewRoleInheritance(child, parent Role) (RoleInheritance, error) {
	if child.IsZero() || parent.IsZero() {
		return RoleInheritance{}, ErrEmptyRole
	}
	if child.String() == parent.String() {
		return RoleInheritance{}, ErrSelfInheritance
	}
	return RoleInheritance{Child: child, Parent: parent}, nil
}

// PolicyRepo 是授权策略的仓储接口，由基础设施层适配到具体判定引擎。
//
// 领域层不依赖 casbin 包：判定引擎属于基础设施选型，
// 日后换实现或加数据权限的 ABAC 模型，领域层不应受影响。
type PolicyRepo interface {
	FindBinding(ctx context.Context, role Role) (*RoleBinding, error)
	SaveBinding(ctx context.Context, b *RoleBinding, expectedVersion *int64) (int64, error)
	ListBindings(ctx context.Context) ([]*RoleBinding, error)
	PolicyVersion(ctx context.Context) (int64, error)
	ResolveCodes(ctx context.Context, roles []Role) ([]PermissionCode, error)
	SaveInheritance(ctx context.Context, ri RoleInheritance, expectedVersion *int64) (int64, error)
	ListInheritances(ctx context.Context) ([]RoleInheritance, error)
	DeleteInheritance(ctx context.Context, ri RoleInheritance, expectedVersion *int64) (int64, error)
}
