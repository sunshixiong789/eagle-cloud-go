package infrastructure

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/go-kratos/kratos/v3/transport"
	"go.opentelemetry.io/otel/trace"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/identity"
)

// policyRepo 把 Casbin 判定器适配到领域层定义的 PolicyRepo 接口。
//
// 多这一层的意义在于让 domain 与 application 都不依赖 casbin 包：
// 授权引擎属于基础设施选型，日后换实现或加数据权限的 ABAC 模型时，
// 领域规则与用例编排都不必改动。
type policyRepo struct {
	enforcer *authz.Enforcer
	store    *PolicyStore
}

// NewPolicyRepo 构造策略仓储。
func NewPolicyRepo(enforcer *authz.Enforcer, store *PolicyStore) domain.PolicyRepo {
	return &policyRepo{enforcer: enforcer, store: store}
}

func (r *policyRepo) FindBinding(ctx context.Context, role domain.Role) (*domain.RoleBinding, error) {
	raw, version, err := r.store.RolePermissionsSnapshot(ctx, role.String())
	if err != nil {
		return nil, err
	}

	codes, err := domain.ParsePermissionCodes(raw)
	if err != nil {
		// 库里存着领域层认不出的权限码，说明策略是绕过应用层写进去的。
		// 直接报错而不是静默丢弃——丢弃会让后台显示的权限比实际生效的少。
		return nil, fmt.Errorf("角色 %q 的策略中含非法权限码: %w", role, err)
	}
	binding, err := domain.NewRoleBinding(role, codes)
	if err != nil {
		return nil, err
	}
	return binding.WithRevision(version), nil
}

func (r *policyRepo) SaveBinding(ctx context.Context, b *domain.RoleBinding, expectedVersion *int64) (int64, error) {
	version, err := r.store.ReplaceRolePermissions(ctx, b.Role().String(), b.CodeStrings(), expectedVersion, mutationMeta(ctx))
	return r.commitPolicy(ctx, version, err)
}

func (r *policyRepo) ResolveCodes(ctx context.Context, roles []domain.Role) ([]domain.PermissionCode, error) {
	raw, err := r.enforcer.PermissionsOf(ctx, roleNames(roles))
	if err != nil {
		return nil, err
	}
	return domain.ParsePermissionCodes(raw)
}

func (r *policyRepo) SaveInheritance(ctx context.Context, ri domain.RoleInheritance, expectedVersion *int64) (int64, error) {
	version, err := r.store.AddRoleInheritance(ctx, ri.Child.String(), ri.Parent.String(), expectedVersion, mutationMeta(ctx))
	return r.commitPolicy(ctx, version, err)
}

func (r *policyRepo) PolicyVersion(ctx context.Context) (int64, error) {
	return r.store.PolicyVersion(ctx)
}

func (r *policyRepo) ListInheritances(ctx context.Context) ([]domain.RoleInheritance, error) {
	rules, _, err := r.store.RulesSnapshot(ctx, "g")
	if err != nil {
		return nil, err
	}
	type rolePair struct{ Child, Parent string }
	pairs := make([]rolePair, 0, len(rules))
	for _, rule := range rules {
		if len(rule.Values) >= 2 {
			pairs = append(pairs, rolePair{Child: rule.Values[0], Parent: rule.Values[1]})
		}
	}
	slices.SortFunc(pairs, func(a, b rolePair) int {
		if n := cmp.Compare(a.Child, b.Child); n != 0 {
			return n
		}
		return cmp.Compare(a.Parent, b.Parent)
	})
	out := make([]domain.RoleInheritance, 0, len(pairs))
	for _, pair := range pairs {
		child, err := domain.NewRole(pair.Child)
		if err != nil {
			continue
		}
		parent, err := domain.NewRole(pair.Parent)
		if err != nil {
			continue
		}
		ri, err := domain.NewRoleInheritance(child, parent)
		if err == nil {
			out = append(out, ri)
		}
	}
	return out, nil
}

func (r *policyRepo) DeleteInheritance(ctx context.Context, ri domain.RoleInheritance, expectedVersion *int64) (int64, error) {
	version, err := r.store.DeleteRoleInheritance(ctx, ri.Child.String(), ri.Parent.String(), expectedVersion, mutationMeta(ctx))
	return r.commitPolicy(ctx, version, err)
}

func (r *policyRepo) commitPolicy(ctx context.Context, version int64, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	if err := r.enforcer.ReloadPolicy(ctx); err != nil {
		return version, fmt.Errorf("策略已提交为版本 %d，但本实例重载失败: %w", version, err)
	}
	return version, nil
}

func mutationMeta(ctx context.Context) policyMutationMeta {
	meta := policyMutationMeta{}
	if p, ok := identity.FromContext(ctx); ok {
		meta.actorSubject = p.Subject
		meta.actorClientID = p.ClientID
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		meta.traceID = sc.TraceID().String()
	}
	if tr, ok := transport.FromServerContext(ctx); ok {
		meta.requestID = tr.RequestHeader().Get("X-Request-ID")
	}
	return meta
}

// ListBindings 用一次数据库查询完成全部角色及其直接权限的分组。
func (r *policyRepo) ListBindings(ctx context.Context) ([]*domain.RoleBinding, error) {
	rules, version, err := r.store.RulesSnapshot(ctx, "p")
	if err != nil {
		return nil, fmt.Errorf("list role bindings: %w", err)
	}

	grouped := make(map[string][]string)
	for _, rule := range rules {
		if len(rule.Values) < 2 || rule.Values[0] == "" || rule.Values[1] == "" {
			continue
		}
		grouped[rule.Values[0]] = append(grouped[rule.Values[0]], rule.Values[1])
	}
	names := slices.Sorted(maps.Keys(grouped))

	out := make([]*domain.RoleBinding, 0, len(names))
	for _, n := range names {
		role, err := domain.NewRole(n)
		if err != nil {
			continue
		}
		codes, err := domain.ParsePermissionCodes(grouped[n])
		if err != nil {
			return nil, fmt.Errorf("角色 %q 的策略中含非法权限码: %w", role, err)
		}
		binding, err := domain.NewRoleBinding(role, codes)
		if err != nil {
			return nil, err
		}
		out = append(out, binding.WithRevision(version))
	}
	return out, nil
}

func roleNames(roles []domain.Role) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.String())
	}
	return out
}
