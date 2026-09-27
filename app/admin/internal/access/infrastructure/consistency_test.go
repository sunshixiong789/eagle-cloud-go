package infrastructure

import (
	"context"
	"testing"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	"github.com/eagle-go/eagle/pkg/authz"
)

// 领域层的 PermissionCode.Covers 与 Casbin 的实际判定必须逐条一致。
//
// 两处各有一份匹配实现：领域层那份让「授权是否生效」可以脱离 Casbin
// 单测，也是后台展示权限时的依据；Casbin 那份是运行时真正的守门人。
// 一旦漂移，轻则「后台显示已授权、调用却 403」，重则领域层以为
// 没授权、Casbin 却放行——那就是越权。
//
// 与其靠注释约定同步，不如让这个测试在漂移发生时立刻变红。
// 放在 infrastructure 包是因为生产代码本就同时依赖 domain 与 authz
// （见 policy.go），pkg/authz 反过来依赖服务内部的 domain 是不允许的。
//
// 本用例不需要数据库：判定器用 nil adapter，策略只存在于内存。
func TestDomainCoversMatchesCasbinEnforcement(t *testing.T) {
	// 覆盖精确匹配、末段通配、域级通配、跨域、同形状不同动作等组合
	policies := []string{
		"system:user:add",
		"system:user:*",
		"system:*",
		"billing:invoice:query",
	}
	targets := []string{
		"system:user:add",
		"system:user:edit",
		"system:user:query",
		"system:dict:add",
		"system:dict:remove",
		"billing:invoice:query",
		"billing:invoice:add",
		"other:thing:do",
	}

	for _, p := range policies {
		policyCode, err := domain.NewPermissionCode(p)
		if err != nil {
			t.Fatalf("构造策略权限码 %q: %v", p, err)
		}

		// 每条策略单独建一个判定器，避免策略之间互相影响判定结果
		e, err := authz.NewEnforcer(nil)
		if err != nil {
			t.Fatalf("NewEnforcer: %v", err)
		}
		const role = "probe"
		if err := e.SetRolePermissions(context.Background(), role, []string{p}); err != nil {
			t.Fatalf("SetRolePermissions: %v", err)
		}

		for _, tgt := range targets {
			targetCode, err := domain.NewPermissionCode(tgt)
			if err != nil {
				t.Fatalf("构造目标权限码 %q: %v", tgt, err)
			}

			domainSays := policyCode.Covers(targetCode)

			casbinSays, err := e.Allow([]string{role}, tgt)
			if err != nil {
				t.Fatalf("Allow(%q, %q): %v", p, tgt, err)
			}

			if domainSays != casbinSays {
				t.Errorf(
					"策略 %q 对 %q 的判定不一致: domain.Covers=%v, casbin.Allow=%v\n"+
						"两处匹配语义已漂移，必须同步 domain.PermissionCode.Covers 与 authz.ModelText",
					p, tgt, domainSays, casbinSays)
			}
		}
	}
}
