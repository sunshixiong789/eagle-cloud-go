package infrastructure

import (
	"context"
	"log/slog"
	"time"

	"github.com/eagle-go/eagle/pkg/authz"
)

const policyReconcileInterval = 5 * time.Second

// runPolicyReconciler 比对数据库版本与内存版本，各副本最多在一个
// 对账周期内追上数据库中的权威策略。
func runPolicyReconciler(
	ctx context.Context,
	store *PolicyStore,
	enforcer *authz.Enforcer,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(policyReconcileInterval)
	defer ticker.Stop()

	reconcile := func() {
		version, err := store.PolicyVersion(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.ErrorContext(ctx, "authz: 对账时读取策略版本失败", "error", err)
			}
			return
		}
		recordPolicyVersionLag(ctx, version, enforcer.LoadedPolicyVersion())
		if version == enforcer.LoadedPolicyVersion() {
			return
		}
		if err := enforcer.ReloadPolicy(ctx); err != nil {
			if ctx.Err() == nil {
				logger.ErrorContext(ctx, "authz: 对账重载策略失败", "error", err, "database_version", version, "loaded_version", enforcer.LoadedPolicyVersion())
			}
			return
		}
		logger.InfoContext(ctx, "authz: 策略版本已自动追平", "version", version)
	}

	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}
