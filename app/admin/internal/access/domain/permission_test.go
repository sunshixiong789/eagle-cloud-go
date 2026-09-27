package domain

import (
	"errors"
	"testing"
	"time"
)

func validParams() NewPermissionParams {
	return NewPermissionParams{
		ParentID: RootPermissionID,
		Name:     "用户管理",
		Code:     "system:user:list",
		Type:     int32(PermissionTypeMenu),
		Status:   int32(StatusEnabled),
		Visible:  true,
	}
}

// 不变量由实体自己守护：构造不出违反规则的 Permission。
func TestNewPermissionEnforcesInvariants(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*NewPermissionParams)
		wantErr error
	}{
		{"合法节点", func(*NewPermissionParams) {}, nil},
		{
			"名称为空",
			func(p *NewPermissionParams) { p.Name = "" },
			ErrEmptyPermissionName,
		},
		{
			"类型越界",
			func(p *NewPermissionParams) { p.Type = 9 },
			ErrInvalidPermissionType,
		},
		{
			// 按钮不挂权限码就永远无法被授权，是个点不动的死按钮，
			// 且在运行时表现为「有按钮但一直 403」，很难排查
			"按钮缺少权限码",
			func(p *NewPermissionParams) {
				p.Type = int32(PermissionTypeButton)
				p.Code = ""
			},
			ErrButtonRequiresCode,
		},
		{
			"目录可以没有权限码",
			func(p *NewPermissionParams) {
				p.Type = int32(PermissionTypeDir)
				p.Code = ""
			},
			nil,
		},
		{
			"权限码格式非法",
			func(p *NewPermissionParams) { p.Code = "bad-code" },
			ErrInvalidPermissionCode,
		},
		{
			"导航节点不能使用通配权限",
			func(p *NewPermissionParams) { p.Code = "system:*" },
			ErrInvalidPermissionCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := validParams()
			tt.mutate(&params)

			_, err := NewPermission(params)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("不应报错, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// 更新失败时实体不得被改坏一半。
func TestPermissionUpdateIsAtomic(t *testing.T) {
	p, err := NewPermission(validParams())
	if err != nil {
		t.Fatalf("NewPermission: %v", err)
	}
	originalName := p.Name()

	bad := validParams()
	bad.Name = "新名字"
	bad.Type = 99 // 非法

	if err := p.Update(bad); err == nil {
		t.Fatal("非法类型应导致更新失败")
	}
	if p.Name() != originalName {
		t.Errorf("更新失败后名称不应被改动: %q -> %q", originalName, p.Name())
	}
}

func TestPermissionEnsureDeletable(t *testing.T) {
	p, err := NewPermission(validParams())
	if err != nil {
		t.Fatalf("NewPermission: %v", err)
	}

	if err := p.EnsureDeletable(0); err != nil {
		t.Errorf("无子节点时应可删除, got %v", err)
	}
	if err := p.EnsureDeletable(3); !errors.Is(err, ErrPermissionHasChildren) {
		t.Errorf("有子节点时应返回 ErrPermissionHasChildren, got %v", err)
	}
}

// ── 权限树 ────────────────────────────────────────────────

// 构造一棵：1(目录) -> 100(菜单) -> 101(按钮)，另有 200(菜单) 挂在 1 下
func snap(id, parent int64, name, code string, typ PermissionType, status, sort int32) PermissionSnapshot {
	now := time.Now()
	return PermissionSnapshot{
		ID: id, ParentID: parent, Name: name, Code: code,
		Type: int32(typ), Status: status, Sort: sort, Visible: true,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
}

func mustRehydratePermission(s PermissionSnapshot) *Permission {
	p, err := RehydratePermission(s)
	if err != nil {
		panic(err)
	}
	return p
}

func buildTree() *PermissionTree {
	return NewPermissionTree([]*Permission{
		mustRehydratePermission(snap(1, 0, "系统管理", "", PermissionTypeDir, 1, 1)),
		mustRehydratePermission(snap(100, 1, "用户管理", "system:user:list", PermissionTypeMenu, 1, 1)),
		mustRehydratePermission(snap(101, 100, "用户新增", "system:user:add", PermissionTypeButton, 1, 1)),
		mustRehydratePermission(snap(200, 1, "字典管理", "system:dict:list", PermissionTypeMenu, 1, 2)),
		mustRehydratePermission(snap(201, 200, "字典停用项", "system:dict:add", PermissionTypeButton, 0, 1)),
		mustRehydratePermission(snap(300, 1, "停用角色菜单", "system:role:list", PermissionTypeMenu, 0, 3)),
	})
}

func TestRehydratePermissionRejectsCorruptSnapshot(t *testing.T) {
	_, err := RehydratePermission(snap(1, 0, "", "", PermissionTypeDir, 1, 1))
	if !errors.Is(err, ErrEmptyPermissionName) {
		t.Fatalf("err = %v, want %v", err, ErrEmptyPermissionName)
	}
}

func TestPermissionTreeEnsureNoCycle(t *testing.T) {
	tree := buildTree()

	tests := []struct {
		name       string
		id, parent int64
		wantErr    error
	}{
		{"挂到根下合法", 100, RootPermissionID, nil},
		{"挂到兄弟下合法", 100, 200, nil},
		{"挂到自身形成环", 100, 100, ErrPermissionCycle},
		{"挂到自身的后代形成环", 1, 101, ErrPermissionCycle},
		{"挂到直接子节点形成环", 100, 101, ErrPermissionCycle},
		{"父节点不存在", 100, 99999, ErrPermissionNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tree.EnsureNoCycle(tt.id, tt.parent)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("不应报错, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// 只持有子节点权限时，祖先目录也必须出现，否则前端挂不上树，
// 表现为「有权限却看不到入口」。
func TestPermissionTreeVisibleMenusIncludesAncestors(t *testing.T) {
	tree := buildTree()

	// 只授予按钮权限 system:user:add（节点 101）
	menus := tree.VisibleMenus([]PermissionCode{MustPermissionCode("system:user:add")})

	ids := make(map[int64]bool, len(menus))
	for _, m := range menus {
		ids[m.ID()] = true
	}

	if !ids[1] {
		t.Error("祖先目录 1 应被补全")
	}
	if !ids[100] {
		t.Error("父菜单 100 应被补全")
	}
	// 按钮本身不进菜单树，前端用权限码单独控制显隐
	if ids[101] {
		t.Error("按钮节点不应出现在菜单树中")
	}
	// 无权限的分支不应出现
	if ids[200] {
		t.Error("未授权的分支 200 不应出现")
	}
}

func visibleMenuIDs(menus []*Permission) map[int64]bool {
	ids := make(map[int64]bool, len(menus))
	for _, m := range menus {
		ids[m.ID()] = true
	}
	return ids
}

// 末段通配授权必须与鉴权判定一致：system:* 让匹配的启用菜单及其祖先可见。
func TestPermissionTreeVisibleMenusWildcardGrant(t *testing.T) {
	tree := buildTree()

	menus := tree.VisibleMenus([]PermissionCode{MustPermissionCode("system:*")})
	ids := visibleMenuIDs(menus)

	if !ids[1] {
		t.Error("祖先目录 1 应被补全")
	}
	if !ids[100] {
		t.Error("system:* 应覆盖启用菜单 system:user:list")
	}
	if !ids[200] {
		t.Error("system:* 应覆盖启用菜单 system:dict:list")
	}
	if ids[101] {
		t.Error("按钮节点不应出现在菜单树中")
	}
	if ids[201] {
		t.Error("停用按钮即使被通配覆盖也不应出现")
	}
	if ids[300] {
		t.Error("停用菜单即使被通配覆盖也不应出现")
	}
}

func TestPermissionTreeVisibleMenusResourceWildcardExcludesOtherBranch(t *testing.T) {
	tree := buildTree()

	menus := tree.VisibleMenus([]PermissionCode{MustPermissionCode("system:user:*")})
	ids := visibleMenuIDs(menus)

	if !ids[1] || !ids[100] {
		t.Error("system:user:* 应带出用户菜单及其祖先")
	}
	if ids[200] {
		t.Error("system:user:* 不应带出字典分支")
	}
	if ids[300] {
		t.Error("停用菜单不应出现")
	}
}

// 停用的节点即便有权限也不应出现在菜单里。
func TestPermissionTreeVisibleMenusSkipsDisabled(t *testing.T) {
	tree := buildTree()

	// 节点 201 的 status 是 0（停用）
	menus := tree.VisibleMenus([]PermissionCode{MustPermissionCode("system:dict:add")})

	for _, m := range menus {
		if m.ID() == 201 {
			t.Error("停用节点不应出现")
		}
	}
	// 停用节点也不该把祖先带出来
	if len(menus) != 0 {
		t.Errorf("仅有停用节点的权限时菜单应为空, got %d 项", len(menus))
	}
}
