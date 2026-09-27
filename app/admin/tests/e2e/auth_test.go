package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/eagle-go/eagle/pkg/identity"
)

// get 带可选 token 发起请求，返回状态码与响应体。
func (e *testEnv) get(t *testing.T, path, token string) (int, string) {
	t.Helper()
	return e.do(t, http.MethodGet, path, token, "")
}

func (e *testEnv) do(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()

	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.http.URL+path, r)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.http.Client().Do(req)
	if err != nil {
		t.Fatalf("发起请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (e *testEnv) doRaw(t *testing.T, method, path, token, contentType string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, e.http.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Clone(), data
}

// 服务能不能起来、路由通不通——这是此前从未验证过的部分。
// 上一轮的 Duration 配置缺陷正是因为从没跑过完整启动路径才漏掉的。
func TestServiceBootsAndRoutes(t *testing.T) {
	env := newTestEnv(t)

	// 未带 token 访问受保护接口，应当是 401 而不是 404 或 500。
	// 404 说明路由没注册，500 说明中间件链装配有问题。
	code, body := env.get(t, "/v1/system/permissions", "")
	if code != http.StatusUnauthorized {
		t.Errorf("未认证请求 = %d (%s), want 401", code, body)
	}
}

func TestUnauthenticatedIsRejected(t *testing.T) {
	env := newTestEnv(t)

	cases := []struct {
		name  string
		token string
	}{
		{"无 token", ""},
		{"伪造签名", env.kc.mint(t, tokenOpts{
			subject: "s1", username: "mallory",
			realmRole: []string{adminRole}, wrongKey: true,
		})},
		{"已过期", env.kc.mint(t, tokenOpts{
			subject: "s2", username: "alice",
			realmRole: []string{adminRole}, expiresIn: -time.Hour,
		})},
		{"aud 不匹配", env.kc.mint(t, tokenOpts{
			subject: "s3", username: "alice",
			realmRole: []string{adminRole}, audience: []string{"another-service"},
		})},
		{"尚未生效", env.kc.mint(t, tokenOpts{
			subject: "s4", username: "alice",
			realmRole: []string{adminRole}, notBefore: 10 * time.Minute,
		})},
		{"非白名单签名算法", env.kc.mint(t, tokenOpts{
			subject: "s5", username: "alice",
			realmRole: []string{adminRole}, algorithm: jose.PS256,
		})},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := env.get(t, "/v1/system/permissions", c.token)
			if code != http.StatusUnauthorized {
				t.Errorf("= %d (%s), want 401", code, body)
			}
		})
	}
}

// 这是整套鉴权的核心语义：角色对了才放行，错了必须 403 而不是 401。
// 401 与 403 混淆会让前端无法区分「该重新登录」和「你没这个权限」。
func TestAuthorizationByRole(t *testing.T) {
	env := newTestEnv(t)

	// viewer 只给读权限，不给写
	env.grantRole(t, "viewer", "system:permission:list", "system:permission:query")
	env.grantRole(t, "editor", "system:permission:list", "system:permission:add")

	t.Run("有权限则放行", func(t *testing.T) {
		token := env.kc.userToken(t, "vera", "viewer")
		code, body := env.get(t, "/v1/system/permissions", token)
		if code != http.StatusOK {
			t.Errorf("viewer 读权限列表 = %d (%s), want 200", code, body)
		}
	})

	t.Run("缺权限则 403", func(t *testing.T) {
		token := env.kc.userToken(t, "vera", "viewer")
		code, body := env.do(t, http.MethodPost, "/v1/system/permissions", token,
			`{"name":"测试","type":1}`)
		if code != http.StatusForbidden {
			t.Errorf("viewer 尝试新增 = %d (%s), want 403", code, body)
		}
	})

	t.Run("换个有权限的角色就通过", func(t *testing.T) {
		token := env.kc.userToken(t, "eddie", "editor")
		code, body := env.do(t, http.MethodPost, "/v1/system/permissions", token,
			`{"name":"E2E 测试节点","type":1}`)
		if code != http.StatusOK {
			t.Errorf("editor 新增 = %d (%s), want 200", code, body)
		}
	})

	t.Run("无任何角色一律 403", func(t *testing.T) {
		token := env.kc.userToken(t, "nobody")
		code, body := env.get(t, "/v1/system/permissions", token)
		if code != http.StatusForbidden {
			t.Errorf("无角色用户 = %d (%s), want 403", code, body)
		}
	})
}

// 超管走短路分支，不查 Casbin。这条路径独立于策略表，
// 必须单独验证——策略表为空时它仍应放行。
func TestSuperAdminBypassesPolicy(t *testing.T) {
	env := newTestEnv(t)

	// 刻意不给 admin 配任何策略
	env.grantRole(t, adminRole)

	token := env.kc.mint(t, tokenOpts{
		subject:  "sub-root",
		username: "root",
		clientRoles: map[string][]string{
			clientID: {adminRole},
		},
	})
	code, body := env.get(t, "/v1/system/permissions", token)
	if code != http.StatusOK {
		t.Errorf("超管读取 = %d (%s), want 200", code, body)
	}
}

// Keycloak 把 client 级角色放在 resource_access.<clientId>.roles。
// 只读 realm_access 会让按 client 授权的角色静默失效——
// 这类错误不会报错，只会表现为「配了权限却还是 403」。
func TestClientRolesFromResourceAccess(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, identity.ClientRoleKey(clientID, "client-viewer"), "system:permission:list")

	token := env.kc.mint(t, tokenOpts{
		subject:  "sub-crv",
		username: "crv",
		// realm 角色为空，权限只来自 client 角色
		clientRoles: map[string][]string{
			clientID: {"client-viewer"},
		},
	})

	code, body := env.get(t, "/v1/system/permissions", token)
	if code != http.StatusOK {
		t.Errorf("client 角色应生效 = %d (%s), want 200", code, body)
	}
}

// 别的客户端的角色不该被本服务采纳，否则等于跨服务越权。
func TestOtherClientRolesAreIgnored(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "foreign-role", "system:permission:list")

	token := env.kc.mint(t, tokenOpts{
		subject:  "sub-foreign",
		username: "foreign",
		clientRoles: map[string][]string{
			"some-other-service": {"foreign-role"},
		},
	})

	code, body := env.get(t, "/v1/system/permissions", token)
	if code != http.StatusForbidden {
		t.Errorf("其他客户端的角色不应生效 = %d (%s), want 403", code, body)
	}
}

func TestRealmAndClientRoleNamesDoNotCollide(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, identity.ClientRoleKey(clientID, "operator"), "system:permission:list")

	realmToken := env.kc.userToken(t, "realm-operator", "operator")
	if code, body := env.get(t, "/v1/system/permissions", realmToken); code != http.StatusForbidden {
		t.Fatalf("同名 realm role = %d (%s), want 403", code, body)
	}

	clientToken := env.kc.mint(t, tokenOpts{
		subject: "sub-client-operator", username: "client-operator",
		clientRoles: map[string][]string{clientID: {"operator"}},
	})
	if code, body := env.get(t, "/v1/system/permissions", clientToken); code != http.StatusOK {
		t.Fatalf("目标 client role = %d (%s), want 200", code, body)
	}
}

// 服务账号令牌（client_credentials）与用户令牌走同一套判定，
// 角色对了就该放行——服务间调用不该需要另一套授权语义。
func TestServiceAccountToken(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "worker", "system:permission:list")

	token := env.kc.serviceToken(t, "eagle-worker", "worker")
	code, body := env.get(t, "/v1/system/permissions", token)
	if code != http.StatusOK {
		t.Errorf("服务账号读取 = %d (%s), want 200", code, body)
	}
}

// 权限变更后重载策略应立即生效，不必重启服务。
func TestPolicyReloadTakesEffect(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "dynamic", "system:permission:list")

	token := env.kc.userToken(t, "dyn", "dynamic")
	if code, _ := env.get(t, "/v1/system/permissions", token); code != http.StatusOK {
		t.Fatal("初始应有权限")
	}

	// 收回权限
	env.grantRole(t, "dynamic")

	if code, body := env.get(t, "/v1/system/permissions", token); code != http.StatusForbidden {
		t.Errorf("收权后 = %d (%s), want 403", code, body)
	}
}

// 错误响应必须是结构化 JSON 且带 reason，前端据此分支处理。
func TestErrorResponseShape(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "viewer", "system:permission:list")

	token := env.kc.userToken(t, "vera", "viewer")
	code, body := env.do(t, http.MethodPost, "/v1/system/permissions", token,
		`{"name":"x","type":3,"code":"a:b:c"}`)
	if code != http.StatusForbidden {
		t.Fatalf("= %d, want 403", code)
	}

	var e struct {
		Code    int    `json:"code"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v (body=%s)", err, body)
	}
	if e.Reason != "FORBIDDEN" {
		t.Errorf("reason = %q, want FORBIDDEN (body=%s)", e.Reason, body)
	}
	if e.Message == "" {
		t.Error("message 为空，前端无从展示")
	}
}
