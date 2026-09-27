package infrastructure

import (
	"context"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	"github.com/eagle-go/eagle/pkg/authz"
)

type authorizationChecker struct {
	enforcer *authz.Enforcer
	store    *PolicyStore
}

func NewAuthorizationChecker(enforcer *authz.Enforcer, store *PolicyStore) domain.AuthorizationChecker {
	return &authorizationChecker{enforcer: enforcer, store: store}
}

func (c *authorizationChecker) Allow(ctx context.Context, roles []string, permission domain.PermissionCode) (bool, error) {
	return c.enforcer.AllowContext(ctx, roles, permission.String())
}

func (c *authorizationChecker) Version(context.Context) (int64, error) {
	// The version must describe the exact in-memory snapshot used by Allow,
	// not a newer database version that the reconciler has not loaded yet.
	return c.enforcer.LoadedPolicyVersion(), nil
}

func (c *authorizationChecker) Snapshot(ctx context.Context) ([]domain.PolicyRule, int64, error) {
	rules, version, err := stablePolicySnapshot(ctx, c.store, c.store.LoadPolicyRows)
	if err != nil {
		return nil, 0, err
	}
	out := make([]domain.PolicyRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, domain.PolicyRule{PType: rule.PType, Values: rule.Values})
	}
	return out, version, nil
}
