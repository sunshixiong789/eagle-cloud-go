package service

import (
	"context"

	"github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/app/admin/internal/access/application"
	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	"github.com/eagle-go/eagle/pkg/identity"
)

// PermissionService 实现 v1.PermissionService。
type PermissionService struct {
	v1.UnimplementedPermissionServiceServer

	uc *application.PermissionUsecase
}

// NewPermissionService 构造权限服务。
func NewPermissionService(uc *application.PermissionUsecase) *PermissionService {
	return &PermissionService{uc: uc}
}

// toProtoPermission 把聚合根投影成传输对象。
// 聚合根的字段是私有的，只能经访问器读取——这正是它能保证不变量的前提。
func toProtoPermission(p *domain.Permission) *v1.Permission {
	if p == nil {
		return nil
	}
	return &v1.Permission{
		Id:        p.ID(),
		ParentId:  p.ParentID(),
		Name:      p.Name(),
		Code:      p.Code().String(),
		Type:      int32(p.Type()),
		Path:      p.Path(),
		Component: p.Component(),
		Icon:      p.Icon(),
		Sort:      p.Sort(),
		Visible:   p.Visible(),
		Status:    fromStatus(p.Status()),
		CreatedAt: ts(p.CreatedAt()),
		UpdatedAt: ts(p.UpdatedAt()),
		Revision:  p.Revision(),
	}
}

// CreatePermission 新建权限节点。
func (s *PermissionService) CreatePermission(ctx context.Context, req *v1.CreatePermissionRequest) (*v1.CreatePermissionResponse, error) {
	p, err := s.uc.CreatePermission(ctx, domain.NewPermissionParams{
		ParentID:  req.GetParentId(),
		Name:      req.GetName(),
		Code:      req.GetCode(),
		Type:      req.GetType(),
		Path:      req.GetPath(),
		Component: req.GetComponent(),
		Icon:      req.GetIcon(),
		Sort:      req.GetSort(),
		Visible:   req.GetVisible(),
		Status:    req.GetStatus(),
	})
	if err != nil {
		return nil, err
	}
	return &v1.CreatePermissionResponse{Permission: toProtoPermission(p)}, nil
}

// GetPermission 查询单个权限节点。
func (s *PermissionService) GetPermission(ctx context.Context, req *v1.GetPermissionRequest) (*v1.GetPermissionResponse, error) {
	p, err := s.uc.GetPermission(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &v1.GetPermissionResponse{Permission: toProtoPermission(p)}, nil
}

// ListPermissions 平铺返回权限列表。
func (s *PermissionService) ListPermissions(ctx context.Context, req *v1.ListPermissionsRequest) (*v1.ListPermissionsResponse, error) {
	perms, err := s.uc.ListPermissions(ctx, domain.ListPermissionsQuery{
		Status: toStatusPtr(req.Status),
		Type:   toPermissionTypePtr(req.Type),
	})
	if err != nil {
		return nil, err
	}

	out := make([]*v1.Permission, 0, len(perms))
	for _, p := range perms {
		out = append(out, toProtoPermission(p))
	}
	return &v1.ListPermissionsResponse{Permissions: out}, nil
}

// UpdatePermission 更新权限节点。
func (s *PermissionService) UpdatePermission(ctx context.Context, req *v1.UpdatePermissionRequest) (*v1.UpdatePermissionResponse, error) {
	p, err := s.uc.UpdatePermission(ctx, req.GetId(), domain.NewPermissionParams{
		ParentID:  req.GetParentId(),
		Name:      req.GetName(),
		Code:      req.GetCode(),
		Type:      req.GetType(),
		Path:      req.GetPath(),
		Component: req.GetComponent(),
		Icon:      req.GetIcon(),
		Sort:      req.GetSort(),
		Visible:   req.GetVisible(),
		Status:    req.GetStatus(),
	}, req.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	return &v1.UpdatePermissionResponse{Permission: toProtoPermission(p)}, nil
}

// DeletePermission 删除权限节点。
func (s *PermissionService) DeletePermission(ctx context.Context, req *v1.DeletePermissionRequest) (*v1.DeletePermissionResponse, error) {
	if err := s.uc.DeletePermission(ctx, req.GetId(), req.ExpectedRevision); err != nil {
		return nil, err
	}
	return &v1.DeletePermissionResponse{}, nil
}

// GetMyMenus 返回当前登录者的菜单树与权限码。
//
// 角色取自 token 而非请求参数：让调用方传角色就等于允许任何人
// 查看任意角色的菜单，进而摸清整个系统的功能边界。
func (s *PermissionService) GetMyMenus(ctx context.Context, _ *v1.GetMyMenusRequest) (*v1.GetMyMenusResponse, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return nil, errors.Unauthorized("UNAUTHENTICATED", "需要登录")
	}

	menus, codes, err := s.uc.GetMenusForRoles(ctx, p.Roles)
	if err != nil {
		return nil, err
	}

	out := make([]*v1.Permission, 0, len(menus))
	for _, m := range menus {
		out = append(out, toProtoPermission(m))
	}
	return &v1.GetMyMenusResponse{
		Menus:           out,
		PermissionCodes: domain.PermissionCodeStrings(codes),
	}, nil
}
