package authz

import (
	"testing"

	annotationsv1 "github.com/eagle-go/eagle/api/eagle/annotations/v1"
	// 空导入以触发 init()，把各模块的文件描述符注册进全局 registry。
	// 没有它，PolicyFor 一律解析不到方法——这正是本测试要守住的前提。
	_ "github.com/eagle-go/eagle/api/eagle/access/v1"
	_ "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
)

func TestPolicyFor(t *testing.T) {
	tests := []struct {
		name       string
		operation  string
		wantPerm   string
		wantPublic bool
		wantKnown  bool
		wantAccess annotationsv1.AccessLevel
	}{
		{
			name:       "声明了权限码的方法",
			operation:  "/eagle.access.v1.PermissionService/CreatePermission",
			wantPerm:   "system:permission:add",
			wantKnown:  true,
			wantAccess: annotationsv1.AccessLevel_ACCESS_LEVEL_PERMISSION_REQUIRED,
		},
		{
			name:       "只需登录、未声明权限码的方法",
			operation:  "/eagle.access.v1.PermissionService/GetMyMenus",
			wantPerm:   "",
			wantKnown:  true,
			wantAccess: annotationsv1.AccessLevel_ACCESS_LEVEL_AUTHENTICATED,
		},
		{
			name:       "角色权限分配",
			operation:  "/eagle.access.v1.RoleBindingService/SetRolePermissions",
			wantPerm:   "system:role:assign",
			wantKnown:  true,
			wantAccess: annotationsv1.AccessLevel_ACCESS_LEVEL_PERMISSION_REQUIRED,
		},
		{
			name:       "查自己的权限只需登录",
			operation:  "/eagle.access.v1.RoleBindingService/GetMyPermissions",
			wantPerm:   "",
			wantKnown:  true,
			wantAccess: annotationsv1.AccessLevel_ACCESS_LEVEL_AUTHENTICATED,
		},
		{
			name:       "字典按类型查询只需登录",
			operation:  "/eagle.dictionary.v1.DictService/GetDictDataByType",
			wantPerm:   "",
			wantKnown:  true,
			wantAccess: annotationsv1.AccessLevel_ACCESS_LEVEL_AUTHENTICATED,
		},
		{
			// gRPC 反射、健康检查等不在本项目契约里的方法，
			// 必须解析为 Known=false，从而落到「需要登录」的保守分支
			name:      "未知服务",
			operation: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
			wantKnown: false,
		},
		{
			name:      "格式非法的 operation",
			operation: "garbage",
			wantKnown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PolicyFor(tt.operation)
			if got.Perm != tt.wantPerm {
				t.Errorf("Perm = %q, want %q", got.Perm, tt.wantPerm)
			}
			if got.Public != tt.wantPublic {
				t.Errorf("Public = %v, want %v", got.Public, tt.wantPublic)
			}
			if got.Known != tt.wantKnown {
				t.Errorf("Known = %v, want %v", got.Known, tt.wantKnown)
			}
			if got.Access != tt.wantAccess {
				t.Errorf("Access = %v, want %v", got.Access, tt.wantAccess)
			}
		})
	}
}

func TestRegisteredPoliciesAreExplicit(t *testing.T) {
	if err := ValidateRegisteredPolicies(nil); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyForIsCached(t *testing.T) {
	const op = "/eagle.access.v1.PermissionService/DeletePermission"

	first := PolicyFor(op)
	second := PolicyFor(op)

	if first != second {
		t.Fatalf("缓存前后结果不一致: %+v vs %+v", first, second)
	}
	if first.Perm != "system:permission:remove" {
		t.Errorf("Perm = %q, want %q", first.Perm, "system:permission:remove")
	}
	if _, ok := policyCache.Load(op); !ok {
		t.Error("查询后 policyCache 中应存在该 operation")
	}
}

func TestSplitOperation(t *testing.T) {
	tests := []struct {
		in          string
		wantService string
		wantMethod  string
		wantOK      bool
	}{
		{"/eagle.dictionary.v1.DictService/CreateDictType", "eagle.dictionary.v1.DictService", "CreateDictType", true},
		{"eagle.dictionary.v1.DictService/CreateDictType", "eagle.dictionary.v1.DictService", "CreateDictType", true},
		{"/OnlyService", "", "", false},
		{"/trailing/", "", "", false},
		{"", "", "", false},
	}

	for _, tt := range tests {
		svc, method, ok := splitOperation(tt.in)
		if ok != tt.wantOK || svc != tt.wantService || method != tt.wantMethod {
			t.Errorf("splitOperation(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, svc, method, ok, tt.wantService, tt.wantMethod, tt.wantOK)
		}
	}
}
