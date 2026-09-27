package authz

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var authzMetrics struct {
	once      sync.Once
	decisions metric.Int64Counter
	reloads   metric.Int64Counter
}

func initAuthzMetrics() {
	authzMetrics.once.Do(func() {
		meter := otel.Meter("github.com/eagle-go/eagle/pkg/authz")
		authzMetrics.decisions, _ = meter.Int64Counter("eagle.authz.decisions")
		authzMetrics.reloads, _ = meter.Int64Counter("eagle.authz.policy_reloads")
	})
}

func recordDecision(ctx context.Context, outcome, permission string) {
	initAuthzMetrics()
	if authzMetrics.decisions != nil {
		authzMetrics.decisions.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", outcome),
			attribute.String("permission", permission),
		))
	}
}

func recordPolicyReload(ctx context.Context, outcome string) {
	initAuthzMetrics()
	if authzMetrics.reloads != nil {
		authzMetrics.reloads.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}
