package authz

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

// Enforcer 封装 Casbin，对外只暴露本项目需要的判定与策略管理接口。
//
// 不直接把 *casbin.Enforcer 暴露给中间件和业务层：Casbin 的 API 面很大，
// 收敛成窄接口后，日后换判定引擎（或加数据权限的 ABAC 模型）
// 不会波及调用方。
type Enforcer struct {
	// Casbin 的 Enforcer 自身对策略读写有锁，但 LoadPolicy 期间
	// 会整体替换内存模型，这里再加一层读写锁以保证重载对判定是原子的。
	mu            sync.RWMutex
	e             *casbin.Enforcer
	adapter       persist.Adapter
	loadedVersion atomic.Int64
}

// NewEnforcer 用给定的策略存储构造判定器。
//
// adapter 传 nil 时策略只存在于内存中，不做持久化。
// 这条路径供测试使用，也适用于策略完全由配置下发、无需运行时修改的部署。
func NewEnforcer(adapter persist.Adapter) (*Enforcer, error) {
	e, err := buildCasbinEnforcer(context.Background(), adapter)
	if err != nil {
		return nil, err
	}

	en := &Enforcer{e: e, adapter: adapter}
	if versioned, ok := adapter.(interface {
		LoadedPolicyVersion() int64
	}); ok {
		en.loadedVersion.Store(versioned.LoadedPolicyVersion())
	}
	return en, nil
}

type contextPolicyLoader interface {
	LoadPolicyContext(context.Context, model.Model) error
}

func buildCasbinEnforcer(ctx context.Context, adapter persist.Adapter) (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(ModelText)
	if err != nil {
		return nil, fmt.Errorf("authz: 解析 Casbin 模型: %w", err)
	}

	// 先创建空判定器，再用携带请求 context 的加载器填充策略。
	// Casbin 的 Adapter 接口本身没有 context 参数，直接调用 LoadPolicy
	// 只能退化成 context.Background，无法响应超时和应用退出。
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("authz: 构造 Casbin enforcer: %w", err)
	}

	if adapter != nil {
		if loader, ok := adapter.(contextPolicyLoader); ok {
			err = loader.LoadPolicyContext(ctx, m)
		} else {
			err = adapter.LoadPolicy(m)
		}
		if err != nil {
			return nil, fmt.Errorf("authz: 加载策略: %w", err)
		}
		e.SetAdapter(adapter)
		// 持久化写入只允许走 Replace* / *IfVersion，避免 Casbin AutoSave
		// 绕过版本号和审计。
		e.EnableAutoSave(false)
		if err := e.BuildRoleLinks(); err != nil {
			return nil, fmt.Errorf("authz: 构建角色继承: %w", err)
		}
	}
	return e, nil
}

// Allow 判断任一角色是否被授予了该权限码。
//
// 逐个角色判定而不是把角色集合传进去，是因为 Casbin 的请求模型是单主体的。
// 角色数量通常个位数，遍历成本可忽略。
func (en *Enforcer) Allow(roles []string, perm string) (bool, error) {
	return en.AllowContext(context.Background(), roles, perm)
}

// AllowContext 与 Allow 相同，但把调用 context 用于指标关联。
func (en *Enforcer) AllowContext(ctx context.Context, roles []string, perm string) (bool, error) {
	en.mu.RLock()
	defer en.mu.RUnlock()

	for _, role := range roles {
		ok, err := en.e.Enforce(role, perm)
		if err != nil {
			recordDecision(ctx, "error", perm)
			return false, fmt.Errorf("authz: 判定角色 %q 对 %q 的权限: %w", role, perm, err)
		}
		if ok {
			recordDecision(ctx, "allowed", perm)
			return true, nil
		}
	}
	recordDecision(ctx, "denied", perm)
	return false, nil
}

// ReloadPolicy 从存储重新加载全部策略。
// 后台调整角色权限后调用，使变更立即生效而不必重启服务。
func (en *Enforcer) ReloadPolicy(ctx context.Context) error {
	replacement, err := buildCasbinEnforcer(ctx, en.adapter)
	if err != nil {
		recordPolicyReload(ctx, "error")
		return err
	}

	en.mu.Lock()
	defer en.mu.Unlock()
	en.e = replacement

	if versioned, ok := en.adapter.(interface {
		LoadedPolicyVersion() int64
	}); ok {
		en.loadedVersion.Store(versioned.LoadedPolicyVersion())
	}
	recordPolicyReload(ctx, "success")
	return nil
}

// ReplacePolicySnapshot builds a complete replacement before taking the write
// lock, so concurrent decisions see either the old or the new policy set.
// It is used by resource services that receive versioned policy snapshots.
func (en *Enforcer) ReplacePolicySnapshot(ctx context.Context, rows []StoredPolicy, version int64) error {
	m, err := model.NewModelFromString(ModelText)
	if err != nil {
		return fmt.Errorf("authz: 解析 Casbin 模型: %w", err)
	}
	for _, row := range rows {
		line := append([]string{row.PType}, row.Values...)
		if err := persist.LoadPolicyArray(line, m); err != nil {
			return fmt.Errorf("authz: load policy %v: %w", line, err)
		}
	}
	replacement, err := casbin.NewEnforcer(m)
	if err != nil {
		return fmt.Errorf("authz: 构造策略快照: %w", err)
	}
	if err := replacement.BuildRoleLinks(); err != nil {
		return fmt.Errorf("authz: 构建角色继承: %w", err)
	}

	en.mu.Lock()
	en.e = replacement
	en.loadedVersion.Store(version)
	en.mu.Unlock()
	recordPolicyReload(ctx, "success")
	return nil
}

// LoadedPolicyVersion 返回当前内存快照对应的数据库版本。
func (en *Enforcer) LoadedPolicyVersion() int64 { return en.loadedVersion.Load() }

// SetRolePermissions 只允许改内存模型，供无持久化适配器的测试使用。
// 生产写入必须走服务 data 层的事务接口。
func (en *Enforcer) SetRolePermissions(_ context.Context, role string, perms []string) error {
	if err := en.guardMemoryOnly(); err != nil {
		return err
	}
	en.mu.Lock()
	defer en.mu.Unlock()

	if _, err := en.e.RemoveFilteredPolicy(0, role); err != nil {
		return fmt.Errorf("authz: 清除角色 %q 的旧策略: %w", role, err)
	}
	if len(perms) == 0 {
		return nil
	}

	rules := make([][]string, 0, len(perms))
	for _, p := range perms {
		rules = append(rules, []string{role, p})
	}
	if _, err := en.e.AddPolicies(rules); err != nil {
		return fmt.Errorf("authz: 写入角色 %q 的策略: %w", role, err)
	}
	return nil
}

// PermissionsOf 汇总若干角色的全部权限码（含继承），结果去重。
// 前端拿它做按钮级显隐。
func (en *Enforcer) PermissionsOf(_ context.Context, roles []string) ([]string, error) {
	en.mu.RLock()
	defer en.mu.RUnlock()

	seen := make(map[string]struct{})
	out := make([]string, 0)

	for _, role := range roles {
		// 用 Implicit 而非 GetFilteredPolicy：后者只返回直接授予的策略，
		// 会漏掉角色继承链上父角色的权限
		implicit, err := en.e.GetImplicitPermissionsForUser(role)
		if err != nil {
			return nil, fmt.Errorf("authz: 展开角色 %q 的权限: %w", role, err)
		}
		for _, rule := range implicit {
			if len(rule) < 2 {
				continue
			}
			if _, dup := seen[rule[1]]; dup {
				continue
			}
			seen[rule[1]] = struct{}{}
			out = append(out, rule[1])
		}
	}
	return out, nil
}

// AddRoleInheritance 只允许改内存模型，供无持久化适配器的测试使用。
func (en *Enforcer) AddRoleInheritance(_ context.Context, child, parent string) error {
	if err := en.guardMemoryOnly(); err != nil {
		return err
	}
	en.mu.Lock()
	defer en.mu.Unlock()

	if _, err := en.e.AddGroupingPolicy(child, parent); err != nil {
		return fmt.Errorf("authz: 建立 %q -> %q 的继承: %w", child, parent, err)
	}
	return nil
}

func (en *Enforcer) guardMemoryOnly() error {
	if en.adapter != nil {
		return fmt.Errorf("authz: 持久化判定器禁止直接改内存策略: %w", ErrAdapterReadOnly)
	}
	return nil
}
