package domain

import "context"

// PolicyRule is the transport-neutral representation of one authorization rule.
type PolicyRule struct {
	PType  string
	Values []string
}

// AuthorizationChecker is the policy decision port owned by access.
type AuthorizationChecker interface {
	Allow(context.Context, []string, PermissionCode) (bool, error)
	Version(context.Context) (int64, error)
	Snapshot(context.Context) ([]PolicyRule, int64, error)
}
