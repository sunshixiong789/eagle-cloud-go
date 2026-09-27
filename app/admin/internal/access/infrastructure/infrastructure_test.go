package infrastructure

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/casbinrule"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/permissiondefinition"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/policyaudit"
	"github.com/eagle-go/eagle/pkg/authz"
)

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
}

// ── 权限树 ────────────────────────────────────────────────

func TestPermissionRepoCRUD(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testDB)
	mustCatalogCode(t, "test:crud:root")

	created, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
		ParentID: domain.RootPermissionID,
		Name:     "测试目录",
		Code:     "test:crud:root",
		Type:     int32(domain.PermissionTypeDir),
		Status:   int32(domain.StatusEnabled),
		Visible:  true,
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, created.ID(), nil) })

	if created.ID() == 0 {
		t.Fatal("创建后应返回自增 ID")
	}
	if created.CreatedAt().IsZero() {
		t.Error("created_at 应被填充")
	}

	got, err := repo.GetByID(ctx, created.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Code().String() != "test:crud:root" {
		t.Errorf("code = %q", got.Code())
	}

	// 聚合根的字段是私有的，只能经领域行为修改——
	// 这保证了不可能绕过不变量校验改坏实体
	if err := got.Update(domain.NewPermissionParams{
		ParentID: domain.RootPermissionID,
		Name:     "改名后",
		Code:     "test:crud:root",
		Type:     int32(domain.PermissionTypeDir),
		Status:   int32(domain.StatusEnabled),
		Visible:  true,
	}); err != nil {
		t.Fatalf("Update 实体: %v", err)
	}

	updated, err := repo.Update(ctx, got, nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name() != "改名后" {
		t.Errorf("name = %q, want 改名后", updated.Name())
	}
}

// newPermission 构造合法的权限聚合根，构造失败直接终止用例。
func newPermission(t *testing.T, params domain.NewPermissionParams) *domain.Permission {
	t.Helper()
	p, err := domain.NewPermission(params)
	if err != nil {
		t.Fatalf("构造 Permission: %v", err)
	}
	return p
}

func mustCatalogCode(t *testing.T, code string) {
	t.Helper()
	parsed := domain.MustPermissionCode(code)
	err := testDB.Client().PermissionDefinition.Create().
		SetCode(parsed.String()).
		SetService(parsed.Domain()).
		SetResource(parsed.Resource()).
		SetAction(parsed.Action()).
		SetStatus(1).
		SetSource("test").
		OnConflictColumns(permissiondefinition.FieldCode).
		Ignore().
		Exec(context.Background())
	if err != nil {
		t.Fatalf("写入权限目录 %s: %v", code, err)
	}
}

// 权限码唯一由条件唯一索引保证，infrastructure 要把它翻译成领域错误。
func TestPermissionRepoDuplicateCode(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testDB)

	// 迁移已种入 system:dict:list
	_, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
		Name:   "重复码",
		Code:   "system:dict:list",
		Type:   int32(domain.PermissionTypeButton),
		Status: int32(domain.StatusEnabled),
	}))
	if !errors.Is(err, domain.ErrPermissionCodeDuplicated) {
		t.Errorf("重复权限码应返回 ErrPermissionCodeDuplicated, got %v", err)
	}
}

// 目录/菜单的 code 可以为空，且多个空值不应互相冲突——
// 这正是用条件唯一索引（WHERE code <> ”）而非唯一约束的原因。
func TestPermissionRepoAllowsMultipleEmptyCodes(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testDB)

	var ids []int64
	for _, name := range []string{"空码目录A", "空码目录B"} {
		p, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
			Name:   name,
			Code:   "",
			Type:   int32(domain.PermissionTypeDir),
			Status: int32(domain.StatusEnabled),
		}))
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		ids = append(ids, p.ID())
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_ = repo.Delete(ctx, id, nil)
		}
	})
}

func TestPermissionRepoNotFound(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testDB)

	if _, err := repo.GetByID(ctx, 999999); !errors.Is(err, domain.ErrPermissionNotFound) {
		t.Errorf("应返回 ErrPermissionNotFound, got %v", err)
	}
	if err := repo.Delete(ctx, 999999, nil); !errors.Is(err, domain.ErrPermissionNotFound) {
		t.Errorf("删除不存在的节点应返回 ErrPermissionNotFound, got %v", err)
	}
}

func TestPermissionRepoRejectsUnknownCatalogCode(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	repo := NewPermissionRepo(testDB)
	_, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
		Name:   "无目录码",
		Code:   "test:unknown:code",
		Type:   int32(domain.PermissionTypeButton),
		Status: int32(domain.StatusEnabled),
	}))
	if !errors.Is(err, domain.ErrUnknownPermissionCode) {
		t.Fatalf("未知目录码应返回 ErrUnknownPermissionCode, got %v", err)
	}
}

func TestPermissionRepoRejectsStaleRevision(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	repo := NewPermissionRepo(testDB)
	mustCatalogCode(t, "test:revision:edit")
	created, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
		Name: "revision-test", Code: "test:revision:edit", Type: int32(domain.PermissionTypeButton), Status: 1,
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, created.ID(), nil) })
	stale := created.Revision() - 1
	if _, err := repo.Update(ctx, created, &stale); !errors.Is(err, domain.ErrConcurrentModification) {
		t.Fatalf("stale revision error = %v", err)
	}
}

func TestPermissionRepoDeleteRejectsNodeWithChildren(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testDB)

	// 种子数据里 id=1 是「系统管理」目录，其下挂着菜单
	if err := repo.Delete(ctx, 1, nil); !errors.Is(err, domain.ErrPermissionHasChildren) {
		t.Fatalf("Delete(parent) = %v, want ErrPermissionHasChildren", err)
	}
	if _, err := repo.GetByID(ctx, 1); err != nil {
		t.Fatalf("有子节点的目录删除失败后应仍存在: %v", err)
	}
}

func TestPermissionRepoListFilters(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testDB)

	all, err := repo.List(ctx, domain.ListPermissionsQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("种子数据应包含权限节点")
	}

	buttonType := domain.PermissionTypeButton
	buttons, err := repo.List(ctx, domain.ListPermissionsQuery{Type: &buttonType})
	if err != nil {
		t.Fatalf("List(buttons): %v", err)
	}
	if len(buttons) == 0 {
		t.Fatal("应能筛出按钮类节点")
	}
	for _, p := range buttons {
		if p.Type() != domain.PermissionTypeButton {
			t.Errorf("筛选结果混入了非按钮节点: %+v", p)
		}
	}
	if len(buttons) >= len(all) {
		t.Error("按类型筛选后数量应少于全量")
	}
}

// ── Casbin 策略 ───────────────────────────────────────────

func newTestPolicyStore(t *testing.T) domain.PolicyRepo {
	t.Helper()

	store := NewPolicyStore(testDB)
	enforcer, err := NewEnforcer(store)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	return NewPolicyRepo(enforcer, store)
}

// mustRoles 构造角色值对象切片。
func mustRoles(t *testing.T, names ...string) []domain.Role {
	t.Helper()
	out := make([]domain.Role, 0, len(names))
	for _, n := range names {
		if !strings.Contains(n, ":") {
			n = "realm:" + n
		}
		r, err := domain.NewRole(n)
		if err != nil {
			t.Fatalf("构造角色 %q: %v", n, err)
		}
		out = append(out, r)
	}
	return out
}

// mustBinding 构造角色权限绑定。
func mustBinding(t *testing.T, role string, codes ...string) *domain.RoleBinding {
	t.Helper()
	if !strings.Contains(role, ":") {
		role = "realm:" + role
	}
	r, err := domain.NewRole(role)
	if err != nil {
		t.Fatalf("构造角色: %v", err)
	}
	cs, err := domain.ParsePermissionCodes(codes)
	if err != nil {
		t.Fatalf("解析权限码: %v", err)
	}
	b, err := domain.NewRoleBinding(r, cs)
	if err != nil {
		t.Fatalf("构造绑定: %v", err)
	}
	return b
}

// 策略必须真的从库里加载出来，而不是只存在于内存。
func TestPolicyStoreLoadsSeededPolicies(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	user := mustRoles(t, "user")

	codes, err := store.ResolveCodes(ctx, user)
	if err != nil {
		t.Fatalf("ResolveCodes: %v", err)
	}
	if !codesCover(codes, "system:dict:query") {
		t.Error("user 角色应拥有 system:dict:query")
	}
	if codesCover(codes, "system:permission:remove") {
		t.Error("user 角色不应拥有 system:permission:remove")
	}
}

// admin 被种入 system:* 通配策略，应覆盖 system 域下全部权限。
func TestPolicyStoreWildcardFromSeed(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	admin := mustRoles(t, "admin")

	codes, err := store.ResolveCodes(ctx, admin)
	if err != nil {
		t.Fatalf("ResolveCodes: %v", err)
	}
	for _, perm := range []string{
		"system:permission:add", "system:dict:remove", "system:role:assign",
	} {
		if !codesCover(codes, perm) {
			t.Errorf("admin 应通过通配策略获得 %q", perm)
		}
	}
}

// 角色继承：迁移里种入了 g, admin, user。
func TestPolicyStoreRoleInheritanceFromSeed(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)

	codes, err := store.ResolveCodes(ctx, mustRoles(t, "admin"))
	if err != nil {
		t.Fatalf("ResolveCodes: %v", err)
	}
	// admin 继承 user，因此应包含 user 的只读权限
	if !slices.Contains(domain.PermissionCodeStrings(codes), "system:dict:query") {
		t.Errorf("admin 应继承 user 的 system:dict:query, got %v", codes)
	}
}

func TestPolicyStoreRejectsRoleInheritanceCycle(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	store := newTestPolicyStore(t)
	a, _ := domain.NewRole("realm:cycle-a")
	b, _ := domain.NewRole("realm:cycle-b")
	first, _ := domain.NewRoleInheritance(a, b)
	if _, err := store.SaveInheritance(ctx, first, nil); err != nil {
		t.Fatalf("SaveInheritance(a->b): %v", err)
	}
	second, _ := domain.NewRoleInheritance(b, a)
	if _, err := store.SaveInheritance(ctx, second, nil); !errors.Is(err, domain.ErrRoleInheritanceCycle) {
		t.Fatalf("cycle error = %v", err)
	}
}

// 策略写入必须真正落库，重启后仍然生效。
func TestPolicyStoreSetRolePermissionsPersists(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	const role = "test-persist-role"

	t.Cleanup(func() {
		_, _ = testDB.Client().CasbinRule.Delete().
			Where(casbinrule.V0EQ(role)).Exec(ctx)
	})

	binding := mustBinding(t, role, "system:dict:query", "system:dict:list")
	if _, err := store.SaveBinding(ctx, binding, nil); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// 用一个全新的 enforcer 从库里重新加载，验证确实持久化了
	fresh := newTestPolicyStore(t)
	got, err := fresh.FindBinding(ctx, mustRoles(t, role)[0])
	if err != nil {
		t.Fatalf("FindBinding: %v", err)
	}

	perms := got.CodeStrings()
	slices.Sort(perms)
	want := []string{"system:dict:list", "system:dict:query"}
	if !slices.Equal(perms, want) {
		t.Errorf("重新加载后的策略 = %v, want %v", perms, want)
	}
}

// 管理端读取必须来自数据库的同一版本快照，不能把旧内存策略配上新版本号。
// 否则另一副本写入后，本副本返回的 revision 看似最新，后续更新却会覆盖新策略。
func TestPolicyRepoFindBindingReadsCurrentDatabaseSnapshot(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := NewPolicyStore(testDB)
	enforcer, err := NewEnforcer(store)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	repo := NewPolicyRepo(enforcer, store)
	role, _ := domain.NewRole("realm:test-snapshot-role")
	t.Cleanup(func() {
		_, _ = testDB.Client().CasbinRule.Delete().Where(casbinrule.V0EQ(role.String())).Exec(ctx)
	})

	version, err := store.ReplaceRolePermissions(
		ctx, role.String(), []string{"system:dict:list"}, nil, policyMutationMeta{},
	)
	if err != nil {
		t.Fatalf("external ReplaceRolePermissions: %v", err)
	}
	if enforcer.LoadedPolicyVersion() == version {
		t.Fatal("test requires an enforcer that has not reloaded the external write")
	}

	got, err := repo.FindBinding(ctx, role)
	if err != nil {
		t.Fatalf("FindBinding: %v", err)
	}
	if got.Revision() != version {
		t.Fatalf("revision = %d, want database version %d", got.Revision(), version)
	}
	if !slices.Equal(got.CodeStrings(), []string{"system:dict:list"}) {
		t.Fatalf("permissions = %v, want current database snapshot", got.CodeStrings())
	}
}

// 全量覆盖语义：收回权限后旧策略不得残留。
func TestPolicyStoreSetRolePermissionsReplaces(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	const role = "test-replace-role"

	t.Cleanup(func() {
		_, _ = testDB.Client().CasbinRule.Delete().
			Where(casbinrule.V0EQ(role)).Exec(ctx)
	})

	if _, err := store.SaveBinding(ctx, mustBinding(t, role, "system:dict:query", "system:dict:add"), nil); err != nil {
		t.Fatalf("首次设置: %v", err)
	}
	if _, err := store.SaveBinding(ctx, mustBinding(t, role, "system:dict:query"), nil); err != nil {
		t.Fatalf("覆盖设置: %v", err)
	}

	fresh := newTestPolicyStore(t)
	codes, err := fresh.ResolveCodes(ctx, mustRoles(t, role))
	if err != nil {
		t.Fatalf("ResolveCodes: %v", err)
	}
	if codesCover(codes, "system:dict:add") {
		t.Error("被覆盖掉的权限不应残留")
	}
	if !codesCover(codes, "system:dict:query") {
		t.Error("保留的权限应仍然有效")
	}
}

// 替换中的新策略写入失败时，删除旧策略也必须一并回滚。
func TestPolicyStoreReplaceIsAtomicOnInsertFailure(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := NewPolicyStore(testDB)
	const role = "test-atomic-role"

	if _, err := store.ReplaceRolePermissions(ctx, role, []string{"system:dict:query"}, nil, policyMutationMeta{}); err != nil {
		t.Fatalf("seed role permissions: %v", err)
	}
	versionBefore, err := store.PolicyVersion(ctx)
	if err != nil {
		t.Fatalf("PolicyVersion: %v", err)
	}

	// Casbin v1 最大 128 字节，强制让批量插入在删除之后失败。
	if _, err := store.ReplaceRolePermissions(ctx, role, []string{strings.Repeat("x", 129)}, nil, policyMutationMeta{}); err == nil {
		t.Fatal("expected replacement failure")
	}
	versionAfter, err := store.PolicyVersion(ctx)
	if err != nil {
		t.Fatalf("PolicyVersion after failure: %v", err)
	}
	if versionAfter != versionBefore {
		t.Fatalf("failed transaction advanced version: before=%d after=%d", versionBefore, versionAfter)
	}

	rows, err := testDB.Client().CasbinRule.Query().
		Where(casbinrule.PtypeEQ("p"), casbinrule.V0EQ(role)).
		All(ctx)
	if err != nil {
		t.Fatalf("query role policies: %v", err)
	}
	if len(rows) != 1 || rows[0].V1 != "system:dict:query" {
		t.Fatalf("old policy was not preserved: %#v", rows)
	}
}

func TestPolicyMutationWritesVersionAndAudit(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := NewPolicyStore(testDB)
	before, err := store.PolicyVersion(ctx)
	if err != nil {
		t.Fatalf("PolicyVersion: %v", err)
	}
	version, err := store.ReplaceRolePermissions(ctx, "test-audit-role", []string{"system:dict:list"}, nil, policyMutationMeta{
		actorSubject:  "subject-1",
		actorClientID: "console",
		requestID:     "request-1",
		traceID:       "0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("ReplaceRolePermissions: %v", err)
	}
	if version <= before {
		t.Fatalf("policy version did not advance: before=%d after=%d", before, version)
	}

	audit, err := testDB.Client().PolicyAudit.Query().Where(policyaudit.PolicyVersionEQ(version)).Only(ctx)
	if err != nil {
		t.Fatalf("query policy audit: %v", err)
	}
	if audit.PolicyVersion != version || audit.ActorSubject != "subject-1" || audit.After[0] != "system:dict:list" {
		t.Fatalf("unexpected audit: %#v", audit)
	}
}

func TestAuthzPolicyReadyIgnoresVersionLag(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := NewPolicyStore(testDB)
	enforcer, err := NewEnforcer(store)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	loaded := enforcer.LoadedPolicyVersion()
	version, err := store.ReplaceRolePermissions(ctx, "test-ready-lag-role", []string{"system:dict:query"}, nil, policyMutationMeta{})
	if err != nil {
		t.Fatalf("ReplaceRolePermissions: %v", err)
	}
	if version == loaded {
		t.Fatalf("expected database version to diverge from loaded version, both=%d", version)
	}
	if got := enforcer.LoadedPolicyVersion(); got != loaded {
		t.Fatalf("loaded version changed without reload: %d -> %d", loaded, got)
	}

	if err := checkAuthzPolicyReady(ctx, store, enforcer); err != nil {
		t.Fatalf("policy readiness with version lag = %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := checkAuthzPolicyReady(canceled, store, enforcer); err == nil {
		t.Fatal("unable to read policy version should still fail readiness")
	}
}

func TestPolicyMutationRejectsStaleExpectedVersion(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := NewPolicyStore(testDB)
	const role = "test-policy-concurrency-role"
	initial, err := store.PolicyVersion(ctx)
	if err != nil {
		t.Fatalf("PolicyVersion: %v", err)
	}

	current, err := store.ReplaceRolePermissions(
		ctx, role, []string{"system:dict:list"}, &initial, policyMutationMeta{},
	)
	if err != nil {
		t.Fatalf("first versioned replacement: %v", err)
	}
	if current <= initial {
		t.Fatalf("policy version did not advance: initial=%d current=%d", initial, current)
	}

	_, err = store.ReplaceRolePermissions(
		ctx, role, []string{"system:dict:remove"}, &initial, policyMutationMeta{},
	)
	if !errors.Is(err, domain.ErrConcurrentModification) {
		t.Fatalf("stale replacement error = %v", err)
	}

	rows, err := testDB.Client().CasbinRule.Query().Where(
		casbinrule.PtypeEQ("p"), casbinrule.V0EQ(role),
	).All(ctx)
	if err != nil {
		t.Fatalf("query role policies: %v", err)
	}
	if len(rows) != 1 || rows[0].V1 != "system:dict:list" {
		t.Fatalf("stale write changed policy: %#v", rows)
	}
}

func TestRoleInheritanceLifecycle(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := NewPolicyStore(testDB)
	const child = "test-lifecycle-child"
	const parent = "test-lifecycle-parent"
	version, err := store.AddRoleInheritance(ctx, child, parent, nil, policyMutationMeta{})
	if err != nil {
		t.Fatalf("add inheritance: %v", err)
	}

	rules, _, err := store.RulesSnapshot(ctx, "g")
	if err != nil {
		t.Fatalf("list inheritances: %v", err)
	}
	found := false
	for _, rule := range rules {
		found = found || len(rule.Values) >= 2 && rule.Values[0] == child && rule.Values[1] == parent
	}
	if !found {
		t.Fatalf("added inheritance not listed: %#v", rules)
	}

	next, err := store.DeleteRoleInheritance(ctx, child, parent, &version, policyMutationMeta{})
	if err != nil {
		t.Fatalf("delete inheritance: %v", err)
	}
	if next <= version {
		t.Fatalf("delete did not advance version: before=%d after=%d", version, next)
	}
	rules, _, err = store.RulesSnapshot(ctx, "g")
	if err != nil {
		t.Fatalf("list inheritances after delete: %v", err)
	}
	for _, rule := range rules {
		if len(rule.Values) >= 2 && rule.Values[0] == child && rule.Values[1] == parent {
			t.Fatal("deleted inheritance is still listed")
		}
	}
}

func TestPolicyStoreListBindings(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)

	bindings, err := store.ListBindings(ctx)
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}

	names := make([]string, 0, len(bindings))
	for _, b := range bindings {
		names = append(names, b.Role().String())
	}

	if !slices.Contains(names, "realm:admin") || !slices.Contains(names, "realm:user") {
		t.Errorf("应包含种子角色 admin 与 user, got %v", names)
	}
	if !slices.IsSorted(names) {
		t.Errorf("结果应有序, got %v", names)
	}
}

func codesCover(codes []domain.PermissionCode, target string) bool {
	want := domain.MustPermissionCode(target)
	for _, c := range codes {
		if c.Covers(want) {
			return true
		}
	}
	return false
}

// 判定器由 infrastructure 装配，确认它能真正构造出来（策略从库加载）。
func TestNewEnforcerLoadsFromDatabase(t *testing.T) {
	skipIfShort(t)

	e, err := NewEnforcer(NewPolicyStore(testDB))
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}

	// 直接调用判定即可证明类型与可用性，无需额外的类型断言
	ok, err := e.Allow([]string{"realm:user"}, "system:dict:list")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Error("应从库中加载到种子策略")
	}

	if err := e.SetRolePermissions(context.Background(), "realm:user", []string{"system:dict:list"}); !errors.Is(err, authz.ErrAdapterReadOnly) {
		t.Fatalf("持久化判定器直接改内存策略: %v", err)
	}
}
