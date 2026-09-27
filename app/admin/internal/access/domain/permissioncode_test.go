package domain

import (
	"errors"
	"testing"
)

// 领域层不依赖任何基础设施，这些用例毫秒级跑完、无需数据库。
// 这正是把不变量下沉到领域层换来的回报。

func TestNewPermissionCode(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"标准三段式", "system:user:add", false},
		{"域级通配", "system:*", false},
		{"资源级通配", "system:user:*", false},
		{"空串代表目录/菜单", "", false},
		{"段数不足", "system:user", true},
		{"段数过多", "a:b:c:d", true},
		{"中间段为空", "system::add", true},
		{"首段为空", ":user:add", true},
		{"半通配不允许", "sys*:user:add", true},
		{"半通配在动作段", "system:user:ad*", true},
		// Casbin 的 keyMatch 会忽略第一个 * 之后的内容，
		// system:*:add 实际等价于 system:*，会连 edit 一起放行。
		// 构造阶段就拒绝，避免这种静默扩权。
		{"通配不在末段", "system:*:add", true},
		{"多个通配段", "system:*:*", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPermissionCode(tt.in)
			if tt.wantErr && err == nil {
				t.Errorf("NewPermissionCode(%q) 应报错", tt.in)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("NewPermissionCode(%q) 不应报错, got %v", tt.in, err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidPermissionCode) {
				t.Errorf("错误应可归类为 ErrInvalidPermissionCode, got %v", err)
			}
		})
	}
}

func TestNewPermissionCodeRejectsNonContractCharacters(t *testing.T) {
	for _, code := range []string{"system:user:read all", "system:user:read-all", "系统:user:list", "1system:user:list"} {
		if _, err := NewPermissionCode(code); err == nil {
			t.Errorf("NewPermissionCode(%q) should fail", code)
		}
	}
}

func TestPermissionCodeSegments(t *testing.T) {
	c := MustPermissionCode("system:user:add")

	if c.Domain() != "system" {
		t.Errorf("Domain = %q, want system", c.Domain())
	}
	if c.Resource() != "user" {
		t.Errorf("Resource = %q, want user", c.Resource())
	}
	if c.Action() != "add" {
		t.Errorf("Action = %q, want add", c.Action())
	}

	// 零值不应 panic
	var zero PermissionCode
	if zero.Domain() != "" || zero.Action() != "" {
		t.Error("零值权限码的各段应为空串")
	}
	if !zero.IsZero() {
		t.Error("零值应被识别为 IsZero")
	}
}

// 值对象的相等性由值决定，可以直接比较。
func TestPermissionCodeIsComparable(t *testing.T) {
	a := MustPermissionCode("system:user:add")
	b := MustPermissionCode("system:user:add")
	c := MustPermissionCode("system:user:edit")

	if a != b {
		t.Error("相同值的权限码应相等")
	}
	if a == c {
		t.Error("不同值的权限码不应相等")
	}
}

// Covers 的语义必须与 Casbin 模型里的 keyMatch 一致：
// 只有整段通配才扩大范围，否则退化为精确相等。
// 两处一旦不同步，就会出现「后台显示已授权、实际调用 403」的割裂。
func TestPermissionCodeCovers(t *testing.T) {
	tests := []struct {
		policy string
		target string
		want   bool
	}{
		{"system:user:add", "system:user:add", true},
		{"system:user:add", "system:user:edit", false},

		// 关键回归：同形状的权限码绝不能互相匹配。
		// 曾用 keyMatch2 导致 system:user:query 被当成
		// system:{任意}:{任意}，只读角色由此获得写权限。
		{"system:user:query", "system:user:add", false},
		{"system:permission:query", "system:dict:remove", false},

		// 末段通配
		{"system:user:*", "system:user:add", true},
		{"system:user:*", "system:user:edit", true},
		{"system:user:*", "system:dict:add", false},

		// 域级通配覆盖整个域
		{"system:*", "system:user:add", true},
		{"system:*", "system:dict:remove", true},

		// 跨域不覆盖
		{"system:user:*", "billing:user:add", false},
		{"system:*", "billing:invoice:add", false},

		// 零值不覆盖任何东西，也不被覆盖
		{"", "system:user:add", false},
		{"system:user:add", "", false},
	}

	for _, tt := range tests {
		policy := PermissionCode{value: tt.policy}
		target := PermissionCode{value: tt.target}

		if got := policy.Covers(target); got != tt.want {
			t.Errorf("(%q).Covers(%q) = %v, want %v", tt.policy, tt.target, got, tt.want)
		}
	}
}

func TestParsePermissionCodes(t *testing.T) {
	got, err := ParsePermissionCodes([]string{"system:user:add", "", "system:user:query"})
	if err != nil {
		t.Fatalf("ParsePermissionCodes: %v", err)
	}
	// 空串代表无权限码的目录，应被跳过而不是产生零值元素
	if len(got) != 2 {
		t.Errorf("应跳过空串, got %d 个", len(got))
	}

	// 任一非法则整体失败——「全量覆盖角色权限」要么全成、要么不动
	if _, err := ParsePermissionCodes([]string{"system:user:add", "bad"}); err == nil {
		t.Error("含非法权限码时应整体失败")
	}
}
