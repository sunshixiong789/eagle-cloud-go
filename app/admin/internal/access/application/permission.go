package application

import (
	"context"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
)

// PermissionUsecase 编排权限用例：构造/变更实体后交给仓储落库。
// 防环、父节点存在、无子节点才能删——这些检查在 data 的树锁里做，避免 TOCTOU。
type PermissionUsecase struct {
	repo   domain.PermissionRepo
	policy domain.PolicyRepo
}

func NewPermissionUsecase(repo domain.PermissionRepo, policy domain.PolicyRepo) *PermissionUsecase {
	return &PermissionUsecase{repo: repo, policy: policy}
}

func (uc *PermissionUsecase) CreatePermission(ctx context.Context, params domain.NewPermissionParams) (*domain.Permission, error) {
	perm, err := domain.NewPermission(params)
	if err != nil {
		return nil, err
	}
	return uc.repo.Create(ctx, perm)
}

func (uc *PermissionUsecase) GetPermission(ctx context.Context, id int64) (*domain.Permission, error) {
	return uc.repo.GetByID(ctx, id)
}

func (uc *PermissionUsecase) ListPermissions(ctx context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	return uc.repo.List(ctx, q)
}

func (uc *PermissionUsecase) UpdatePermission(ctx context.Context, id int64, params domain.NewPermissionParams, expectedRevision *int64) (*domain.Permission, error) {
	current, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := current.Update(params); err != nil {
		return nil, err
	}
	return uc.repo.Update(ctx, current, expectedRevision)
}

func (uc *PermissionUsecase) DeletePermission(ctx context.Context, id int64, expectedRevision *int64) error {
	return uc.repo.Delete(ctx, id, expectedRevision)
}

// GetMenusForRoles 返回给定角色可见的菜单与全部权限码。
//
// 菜单与鉴权判定共用同一份 Casbin 策略，因此不会出现
// 「菜单看得见但点了 403」这种前后端权限口径不一致的情况。
func (uc *PermissionUsecase) GetMenusForRoles(ctx context.Context, roleNames []string) ([]*domain.Permission, []domain.PermissionCode, error) {
	roles, err := toRoles(roleNames)
	if err != nil {
		return nil, nil, err
	}

	codes, err := uc.policy.ResolveCodes(ctx, roles)
	if err != nil {
		return nil, nil, err
	}

	perms, err := uc.repo.List(ctx, domain.ListPermissionsQuery{})
	if err != nil {
		return nil, nil, err
	}
	return domain.NewPermissionTree(perms).VisibleMenus(codes), codes, nil
}

func toRoles(names []string) ([]domain.Role, error) {
	out := make([]domain.Role, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		r, err := domain.NewRole(n)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
