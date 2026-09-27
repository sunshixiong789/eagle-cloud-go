package infrastructure

import (
	"context"
	"log/slog"

	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/healthx"
)

func NewEnforcer(store *PolicyStore) (*authz.Enforcer, error) {
	codes, err := store.PermissionCatalogCodes(context.Background())
	if err != nil {
		return nil, err
	}
	if err := authz.ValidateRegisteredPolicies(codes); err != nil {
		return nil, err
	}
	return authz.NewEnforcer(authz.NewStorageAdapter(store))
}

func RegisterPolicyHealth(store *PolicyStore, enforcer *authz.Enforcer) func() {
	return healthx.Default.Register("authz-policy", func(ctx context.Context) error {
		return checkAuthzPolicyReady(ctx, store, enforcer)
	})
}

func checkAuthzPolicyReady(ctx context.Context, store *PolicyStore, enforcer *authz.Enforcer) error {
	version, err := store.PolicyVersion(ctx)
	if err != nil {
		return err
	}
	recordPolicyVersionLag(ctx, version, enforcer.LoadedPolicyVersion())
	return nil
}

// NewPolicyReconciler starts version reconciliation and returns its cleanup.
func NewPolicyReconciler(store *PolicyStore, enforcer *authz.Enforcer, logger *slog.Logger) func() {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPolicyReconciler(ctx, store, enforcer, logger)
	}()
	return func() {
		cancel()
		<-done
	}
}
