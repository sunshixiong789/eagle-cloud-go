package domain

import "slices"

// PermissionTree 是权限节点的集合视图，承载需要纵观全树才能判断的规则。
//
// 单个聚合根看不到兄弟和祖先，因此「防环」「补全祖先链」放在这里。
type PermissionTree struct {
	byID map[int64]*Permission
	all  []*Permission
}

// NewPermissionTree 用平铺的节点列表构建树视图。
func NewPermissionTree(perms []*Permission) *PermissionTree {
	byID := make(map[int64]*Permission, len(perms))
	for _, p := range perms {
		byID[p.id] = p
	}
	return &PermissionTree{byID: byID, all: perms}
}

// EnsureNoCycle 校验把 id 挂到 newParentID 之下不会形成环。
//
// 上溯 newParentID 的祖先链，遇到 id 即说明 newParentID 是 id 的后代。
// 加深度上限是为了在数据本身已成环时也能终止。
func (t *PermissionTree) EnsureNoCycle(id, newParentID int64) error {
	if newParentID == RootPermissionID {
		return nil
	}
	if newParentID == id {
		return ErrPermissionCycle
	}

	const maxDepth = 64
	cursor := newParentID
	for range maxDepth {
		node, ok := t.byID[cursor]
		if !ok {
			return ErrPermissionNotFound
		}
		if node.parentID == RootPermissionID {
			return nil
		}
		if node.parentID == id {
			return ErrPermissionCycle
		}
		cursor = node.parentID
	}
	return ErrPermissionCycle
}

// VisibleMenus 返回持有 granted 这批权限码的主体可见的菜单节点。
//
// 除了直接命中的节点，还会补全它们的祖先链：
// 少了父目录，子菜单在前端就挂不上树。
func (t *PermissionTree) VisibleMenus(granted []PermissionCode) []*Permission {
	visible := make(map[int64]struct{}, len(t.all))
	for _, p := range t.all {
		if p.code.IsZero() || !p.status.Enabled() {
			continue
		}
		if grantedCovers(granted, p.code) {
			visible[p.id] = struct{}{}
		}
	}

	for id := range visible {
		for cur, ok := t.byID[id]; ok && !cur.IsRoot(); {
			parent, found := t.byID[cur.parentID]
			if !found {
				break
			}
			if _, seen := visible[parent.id]; seen {
				break
			}
			visible[parent.id] = struct{}{}
			cur = parent
		}
	}

	out := make([]*Permission, 0, len(visible))
	for _, p := range t.all {
		if _, ok := visible[p.id]; ok && p.IsMenuNode() {
			out = append(out, p)
		}
	}
	return out
}

// grantedCovers 判断任一已授予的策略是否覆盖目标权限码。
// 与鉴权判定共用 PermissionCode.Covers，因此 system:* 也会让对应菜单可见。
func grantedCovers(granted []PermissionCode, target PermissionCode) bool {
	return slices.ContainsFunc(granted, func(g PermissionCode) bool {
		return g.Covers(target)
	})
}
