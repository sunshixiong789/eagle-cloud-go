// Package domain contains the access module's model, invariants, and ports.
package domain

import "errors"

var (
	ErrInvalidPermissionCode    = errors.New("domain: 权限码格式不合法")
	ErrPermissionNotFound       = errors.New("domain: 权限不存在")
	ErrPermissionCodeDuplicated = errors.New("domain: 权限码已存在")
	ErrPermissionHasChildren    = errors.New("domain: 存在子节点，无法删除")
	ErrPermissionCycle          = errors.New("domain: 上级权限不能是自身或其后代")
	ErrConcurrentModification   = errors.New("domain: 数据已被其他操作更新，请刷新后重试")
	ErrButtonRequiresCode       = errors.New("domain: 按钮类权限必须声明权限码")
	ErrInvalidPermissionType    = errors.New("domain: 权限类型不合法")
	ErrEmptyPermissionName      = errors.New("domain: 权限名称不能为空")
	ErrEmptyRole                = errors.New("domain: 角色名不能为空")
	ErrRoleNotBound             = errors.New("domain: 该角色尚未配置任何权限")
	ErrUnknownPermissionCode    = errors.New("domain: 权限码不在权限目录中")
	ErrSelfInheritance          = errors.New("domain: 角色不能继承自身")
	ErrRoleInheritanceCycle     = errors.New("domain: 角色继承关系不能形成环")
)
