package authz

import (
	"context"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/transport"

	_ "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/pkg/identity"
)

// stubTransport 实现 transport.Transporter，只为让中间件取到 operation。
type stubTransport struct {
	operation string
}

func (s *stubTransport) Kind() transport.Kind            { return transport.KindGRPC }
func (s *stubTransport) Endpoint() string                { return "" }
func (s *stubTransport) Operation() string               { return s.operation }
func (s *stubTransport) RequestHeader() transport.Header { return nil }
func (s *stubTransport) ReplyHeader() transport.Header   { return nil }

func serverCtx(operation string) context.Context {
	return transport.NewServerContext(context.Background(), &stubTransport{operation: operation})
}

func probeHandler(called *bool) func(context.Context, any) (any, error) {
	return func(_ context.Context, _ any) (any, error) {
		*called = true
		return "ok", nil
	}
}

// newTestEnforcer 构造纯内存判定器，并按 policies 写入 p 规则。
func newTestEnforcer(t *testing.T, policies map[string][]string) *Enforcer {
	t.Helper()

	e, err := NewEnforcer(nil)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	for role, perms := range policies {
		if err := e.SetRolePermissions(context.Background(), role, perms); err != nil {
			t.Fatalf("SetRolePermissions(%s): %v", role, err)
		}
	}
	return e
}

const opCreatePermission = "/eagle.access.v1.PermissionService/CreatePermission"
const opCheckPermission = "/eagle.access.v1.AuthorizationService/CheckPermission"

func TestServerInternalAccessRequiresWhitelistedService(t *testing.T) {
	tests := []struct {
		name      string
		principal *identity.Principal
		wantCode  int
	}{
		{name: "human", principal: &identity.Principal{Subject: "u-1", ClientID: "eagle-web"}, wantCode: 403},
		{name: "untrusted service", principal: &identity.Principal{Subject: "s-1", ClientID: "other-worker", IsService: true}, wantCode: 403},
		{name: "trusted service", principal: &identity.Principal{Subject: "s-1", ClientID: "eagle-worker", IsService: true}, wantCode: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			mw := Server(WithInternalClientIDs("eagle-worker"))
			ctx := identity.NewContext(serverCtx(opCheckPermission), tt.principal)
			_, err := mw(probeHandler(&called))(ctx, nil)
			if got := kratoserrors.Code(err); got != tt.wantCode {
				t.Fatalf("状态码 = %d, want %d, err=%v", got, tt.wantCode, err)
			}
			if called != (tt.wantCode == 200) {
				t.Fatalf("handler called = %v, want %v", called, tt.wantCode == 200)
			}
		})
	}
}

func TestServerRejectsAnonymous(t *testing.T) {
	var called bool
	mw := Server(WithEnforcer(newTestEnforcer(t, nil)))

	_, err := mw(probeHandler(&called))(serverCtx(opCreatePermission), nil)

	if kratoserrors.Code(err) != 401 {
		t.Errorf("匿名调用状态码 = %d, want 401", kratoserrors.Code(err))
	}
	if called {
		t.Error("handler 不应被执行")
	}
}

func TestServerRejectsUnknownOperationEvenWhenAuthenticated(t *testing.T) {
	var called bool
	mw := Server(WithEnforcer(newTestEnforcer(t, nil)))
	ctx := identity.NewContext(serverCtx("/unknown.Service/Dangerous"), &identity.Principal{Subject: "u-1"})

	_, err := mw(probeHandler(&called))(ctx, nil)
	if kratoserrors.Code(err) != 403 {
		t.Errorf("未知 RPC 状态码 = %d, want 403", kratoserrors.Code(err))
	}
	if called {
		t.Error("未知 RPC 不应进入 handler")
	}
}

func TestServerRejectsMissingPermission(t *testing.T) {
	var called bool
	mw := Server(WithEnforcer(newTestEnforcer(t, map[string][]string{
		// 只有查询权限，没有新增权限
		"viewer": {"system:permission:query", "system:permission:list"},
	})))

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"viewer"},
	})
	_, err := mw(probeHandler(&called))(ctx, nil)

	if kratoserrors.Code(err) != 403 {
		t.Errorf("状态码 = %d, want 403", kratoserrors.Code(err))
	}
	if called {
		t.Error("handler 不应被执行")
	}
}

func TestServerAllowsWithPermission(t *testing.T) {
	var called bool
	mw := Server(WithEnforcer(newTestEnforcer(t, map[string][]string{
		"editor": {"system:permission:add"},
	})))

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"editor"},
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("持有权限时应放行, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

// 用户可能同时拥有多个角色，任一命中即放行。
func TestServerAllowsWhenAnyRoleGrants(t *testing.T) {
	var called bool
	mw := Server(WithEnforcer(newTestEnforcer(t, map[string][]string{
		"viewer": {"system:permission:query"},
		"editor": {"system:permission:add"},
	})))

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"viewer", "editor"},
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("任一角色命中即应放行, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

// 通配策略：system:* 应覆盖 system 域下的具体权限码。
func TestServerSupportsWildcardPolicy(t *testing.T) {
	var called bool
	mw := Server(WithEnforcer(newTestEnforcer(t, map[string][]string{
		"ops": {"system:*"},
	})))

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"ops"},
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("通配策略应覆盖具体权限码, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

// 回归测试：权限码之间不得因为「形状相同」而互相匹配。
//
// 曾用 keyMatch2 做通配，而它是为 URL 路径设计的，把 `:xxx` 当成路径参数。
// 权限码正好是冒号分隔的，导致 system:permission:query 被解析成
// system:{任意}:{任意}，与 system:permission:add 匹配成功——
// 只读角色由此获得全部写权限。改回 keyMatch 后本用例守住这个边界。
func TestSameShapedPermissionCodesDoNotMatchEachOther(t *testing.T) {
	e := newTestEnforcer(t, map[string][]string{
		"viewer": {"system:permission:query"},
	})

	cases := []struct {
		perm string
		want bool
	}{
		{"system:permission:query", true}, // 精确匹配
		{"system:permission:add", false},  // 同形状但不同动作
		{"system:permission:remove", false},
		{"system:dict:query", false}, // 同形状但不同资源
		{"other:permission:query", false},
	}

	for _, c := range cases {
		got, err := e.Allow([]string{"viewer"}, c.perm)
		if err != nil {
			t.Fatalf("Allow(%q): %v", c.perm, err)
		}
		if got != c.want {
			t.Errorf("Allow(viewer, %q) = %v, want %v", c.perm, got, c.want)
		}
	}
}

// 通配只应在策略侧生效，且不得跨域。
func TestWildcardScopeIsBounded(t *testing.T) {
	e := newTestEnforcer(t, map[string][]string{
		"ops": {"system:*"},
	})

	cases := []struct {
		perm string
		want bool
	}{
		{"system:permission:add", true},
		{"system:dict:remove", true},
		{"internal:auth:verify", false}, // 不同域不应被 system:* 覆盖
		{"billing:invoice:add", false},
	}

	for _, c := range cases {
		got, err := e.Allow([]string{"ops"}, c.perm)
		if err != nil {
			t.Fatalf("Allow(%q): %v", c.perm, err)
		}
		if got != c.want {
			t.Errorf("Allow(ops, %q) = %v, want %v", c.perm, got, c.want)
		}
	}
}

// 角色继承：子角色应自动获得父角色的权限。
func TestServerHonorsRoleInheritance(t *testing.T) {
	e := newTestEnforcer(t, map[string][]string{
		"editor": {"system:permission:add"},
	})
	if err := e.AddRoleInheritance(context.Background(), "lead", "editor"); err != nil {
		t.Fatalf("AddRoleInheritance: %v", err)
	}

	var called bool
	mw := Server(WithEnforcer(e))

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"lead"},
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("继承而来的权限应放行, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

func TestServerSuperAdminShortCircuits(t *testing.T) {
	var called bool
	// 判定器里没有任何策略，超管仍应放行
	mw := Server(
		WithSuperAdminRole("admin"),
		WithEnforcer(newTestEnforcer(t, nil)),
	)

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"admin"}, ClientRoles: []string{"admin"},
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("超管应被放行, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

func TestServerRealmRoleCannotTriggerSuperAdminBypass(t *testing.T) {
	var called bool
	mw := Server(
		WithSuperAdminRole("admin"),
		WithEnforcer(newTestEnforcer(t, nil)),
	)

	// Roles 中的 admin 模拟 realm_access.roles；没有对应 ClientRoles 时
	// 必须继续走 Casbin，并因没有策略而拒绝。
	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1",
		Roles:   []string{"admin"},
	})
	_, err := mw(probeHandler(&called))(ctx, nil)
	if kratoserrors.Code(err) != 403 {
		t.Fatalf("realm admin 状态码 = %d, want 403", kratoserrors.Code(err))
	}
	if called {
		t.Error("realm admin 不应触发本服务超管短路")
	}
}

func TestServerAllowsAuthenticatedWhenNoPermDeclared(t *testing.T) {
	var called bool
	mw := Server(WithEnforcer(newTestEnforcer(t, nil)))

	// GetMyMenus 未声明权限码，登录即可
	ctx := identity.NewContext(
		serverCtx("/eagle.access.v1.PermissionService/GetMyMenus"),
		&identity.Principal{Subject: "u-1"},
	)
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("已登录用户应可访问未声明权限码的方法, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

// 服务账号与终端用户走同一套判定：两者的角色都由 Keycloak 下发。
func TestServerTreatsServiceAccountsLikeUsers(t *testing.T) {
	mw := Server(WithEnforcer(newTestEnforcer(t, map[string][]string{
		"svc-writer": {"system:permission:add"},
	})))

	t.Run("角色命中则放行", func(t *testing.T) {
		var called bool
		ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
			Subject: "svc-uuid", IsService: true, ClientID: "eagle-worker",
			Roles: []string{"svc-writer"},
		})
		if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
			t.Fatalf("服务账号持有角色时应放行, got %v", err)
		}
		if !called {
			t.Error("handler 应被执行")
		}
	})

	t.Run("角色不匹配则 403", func(t *testing.T) {
		var called bool
		ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
			Subject: "svc-uuid", IsService: true, ClientID: "other",
			Roles: []string{"svc-reader"},
		})
		_, err := mw(probeHandler(&called))(ctx, nil)
		if kratoserrors.Code(err) != 403 {
			t.Errorf("状态码 = %d, want 403", kratoserrors.Code(err))
		}
		if called {
			t.Error("handler 不应被执行")
		}
	})
}

// 未配置判定器属于装配错误，必须拒绝而非放行。
func TestServerDeniesWhenEnforcerMissing(t *testing.T) {
	var called bool
	mw := Server()

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"editor"},
	})
	_, err := mw(probeHandler(&called))(ctx, nil)

	if kratoserrors.Code(err) != 403 {
		t.Errorf("状态码 = %d, want 403", kratoserrors.Code(err))
	}
	if called {
		t.Error("handler 不应被执行")
	}
}

// 撤销权限后应立即失效，不能因为策略还在内存里就继续放行。
func TestSetRolePermissionsRevokesImmediately(t *testing.T) {
	e := newTestEnforcer(t, map[string][]string{
		"editor": {"system:permission:add"},
	})
	mw := Server(WithEnforcer(e))

	ctx := identity.NewContext(serverCtx(opCreatePermission), &identity.Principal{
		Subject: "u-1", Roles: []string{"editor"},
	})

	var called bool
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("撤销前应放行, got %v", err)
	}

	// 收回全部权限
	if err := e.SetRolePermissions(context.Background(), "editor", nil); err != nil {
		t.Fatalf("SetRolePermissions: %v", err)
	}

	called = false
	_, err := mw(probeHandler(&called))(ctx, nil)
	if kratoserrors.Code(err) != 403 {
		t.Errorf("撤销后状态码 = %d, want 403", kratoserrors.Code(err))
	}
	if called {
		t.Error("撤销后 handler 不应被执行")
	}
}
