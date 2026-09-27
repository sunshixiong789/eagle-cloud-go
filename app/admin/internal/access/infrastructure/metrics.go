package infrastructure

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var policySyncMetrics struct {
	once       sync.Once
	versionLag metric.Int64Histogram
}

func initPolicySyncMetrics() {
	policySyncMetrics.once.Do(func() {
		meter := otel.Meter("github.com/eagle-go/eagle/app/admin/internal/access/infrastructure")
		policySyncMetrics.versionLag, _ = meter.Int64Histogram("eagle.authz.policy_version_lag")
	})
}

func recordPolicyVersionLag(ctx context.Context, databaseVersion, loadedVersion int64) {
	initPolicySyncMetrics()
	lag := databaseVersion - loadedVersion
	if lag < 0 {
		lag = 0
	}
	if policySyncMetrics.versionLag != nil {
		policySyncMetrics.versionLag.Record(ctx, lag)
	}
}
