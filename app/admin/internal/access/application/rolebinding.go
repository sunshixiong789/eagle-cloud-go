package application

import (
	"context"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
)

// RoleBindingUsecase 是角色权限绑定的应用服务。
type RoleBindingUsecase struct {
	policy   domain.PolicyRepo
	permRepo domain.PermissionRepo
}

// NewRoleBindingUsecase 构造用例。
func NewRoleBindingUsecase(policy domain.PolicyRepo, permRepo domain.PermissionRepo) *RoleBindingUsecase {
	return &RoleBindingUsecase{policy: policy, permRepo: permRepo}
}

// ListBoundRoles 列出已配置权限的角色及其权限码。
func (uc *RoleBindingUsecase) ListBoundRoles(ctx context.Context) ([]*domain.RoleBinding, int64, error) {
	bindings, err := uc.policy.ListBindings(ctx)
	if err != nil {
		return nil, 0, err
	}
	version, err := uc.policy.PolicyVersion(ctx)
	return bindings, version, err
}

// GetRolePermissions 返回单个角色的权限绑定。
func (uc *RoleBindingUsecase) GetRolePermissions(ctx context.Context, roleName string) (*domain.RoleBinding, error) {
	role, err := domain.NewRole(roleName)
	if err != nil {
		return nil, err
	}

	b, err := uc.policy.FindBinding(ctx, role)
	if err != nil {
		return nil, err
	}
	if b.IsEmpty() {
		return nil, domain.ErrRoleNotBound
	}
	return b, nil
}

// SetRolePermissions 全量覆盖角色的权限码。
//
// 写入前校验每个权限码都存在于权限树中：拼错的权限码不会命中任何接口，
// 但配置的人会以为授权成功了——这种静默失败比直接报错难查得多。
// 这是一条跨聚合的规则（RoleBinding 需要 Permission 树的知识），
// 因此由应用层负责把两个聚合的数据凑齐，判定逻辑仍在领域对象上。
func (uc *RoleBindingUsecase) SetRolePermissions(ctx context.Context, roleName string, codeStrings []string, expectedVersion *int64) (int64, error) {
	role, err := domain.NewRole(roleName)
	if err != nil {
		return 0, err
	}

	codes, err := domain.ParsePermissionCodes(codeStrings)
	if err != nil {
		return 0, err
	}

	binding, err := domain.NewRoleBinding(role, codes)
	if err != nil {
		return 0, err
	}

	knownCodes, err := uc.permRepo.KnownCodes(ctx)
	if err != nil {
		return 0, err
	}

	if err := binding.EnsureCodesKnown(knownCodes); err != nil {
		return 0, err
	}

	return uc.policy.SaveBinding(ctx, binding, expectedVersion)
}

// AddRoleInheritance 建立角色继承。
func (uc *RoleBindingUsecase) AddRoleInheritance(ctx context.Context, childName, parentName string, expectedVersion *int64) (int64, error) {
	child, err := domain.NewRole(childName)
	if err != nil {
		return 0, err
	}
	parent, err := domain.NewRole(parentName)
	if err != nil {
		return 0, err
	}

	ri, err := domain.NewRoleInheritance(child, parent)
	if err != nil {
		return 0, err
	}
	return uc.policy.SaveInheritance(ctx, ri, expectedVersion)
}

func (uc *RoleBindingUsecase) ListRoleInheritances(ctx context.Context) ([]domain.RoleInheritance, int64, error) {
	items, err := uc.policy.ListInheritances(ctx)
	if err != nil {
		return nil, 0, err
	}
	version, err := uc.policy.PolicyVersion(ctx)
	return items, version, err
}

func (uc *RoleBindingUsecase) DeleteRoleInheritance(ctx context.Context, childName, parentName string, expectedVersion *int64) (int64, error) {
	child, err := domain.NewRole(childName)
	if err != nil {
		return 0, err
	}
	parent, err := domain.NewRole(parentName)
	if err != nil {
		return 0, err
	}
	ri, err := domain.NewRoleInheritance(child, parent)
	if err != nil {
		return 0, err
	}
	return uc.policy.DeleteInheritance(ctx, ri, expectedVersion)
}

// ResolveCodes 汇总若干角色展开继承后的全部权限码。
func (uc *RoleBindingUsecase) ResolveCodes(ctx context.Context, roleNames []string) ([]domain.PermissionCode, error) {
	roles, err := toRoles(roleNames)
	if err != nil {
		return nil, err
	}
	return uc.policy.ResolveCodes(ctx, roles)
}
