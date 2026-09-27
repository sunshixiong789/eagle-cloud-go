// Package server 装配 HTTP / gRPC 服务器与中间件链。
package server

import (
	"context"
	"fmt"
	"log/slog"

	protovalidatemw "github.com/go-kratos/kratos/contrib/middleware/validate/v3"
	"github.com/go-kratos/kratos/contrib/otel/v3/metrics"
	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/middleware/logging"
	"github.com/go-kratos/kratos/v3/middleware/metadata"
	"github.com/go-kratos/kratos/v3/middleware/ratelimit"
	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"go.opentelemetry.io/otel"

	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

// meterName 是本服务所有自定义指标的 instrumentation scope。
const meterName = "github.com/eagle-go/eagle/pkg/platform/server"

// NewVerifier 构造 Keycloak token 验证器。
func NewVerifier(c *config.Auth) *authn.Verifier {
	return authn.NewVerifier(
		context.Background(),
		authn.Config{
			Issuer:   c.GetIssuer(),
			ClientID: c.GetClientId(),
			Audience: c.GetAudience(),
			// 留空时 authn 会按 Keycloak 约定从 issuer 推导。
			// 容器/K8s 里通常要显式指定：token 里的公开 issuer
			// 与本服务可达的集群内地址往往不是同一个。
			JWKSURL: c.GetJwksUrl(),
		},
	)
}

// NewMiddlewares 构造 HTTP 与 gRPC 共用的中间件链。
//
// 顺序是有讲究的：
//   - recovery 必须在最外层，否则内层 panic 会直接打挂进程
//   - logging 紧随其后，才能记录到后面被拒绝的请求
//   - ratelimit 在鉴权之前：过载保护不该依赖「先把 token 验完」
//   - authn -> authz：先确认你是谁（Keycloak 验签），再判断你能不能做（Casbin）
//   - 参数校验放最内层：只有通过鉴权的请求才值得花这个代价
func NewMiddlewares(
	logger *slog.Logger,
	verifier *authn.Verifier,
	authorizer authz.Authorizer,
	authConf *config.Auth,
	errorMappings ...ErrorMappingRule,
) ([]middleware.Middleware, error) {
	if err := authz.ValidateRegisteredPolicies(nil); err != nil {
		return nil, err
	}
	superAdmin := authConf.GetSuperAdminRole()
	if superAdmin == "" {
		superAdmin = "admin"
	}

	metricsMW, err := newMetricsMiddleware()
	if err != nil {
		return nil, err
	}

	return []middleware.Middleware{
		recovery.Recovery(),
		// tracing 紧贴 recovery：这样后续每一层——包括被拒绝的请求——
		// 都落在同一个 span 里，排查 403 时能看到完整调用链
		tracing.Server(),
		metricsMW,
		logging.Server(logger),
		metadata.Server(metadata.WithPropagatedPrefix("x-md-global-")),
		ratelimit.Server(),
		authn.Server(verifier),
		authz.Server(
			authz.WithSuperAdminRole(superAdmin),
			authz.WithAuthorizer(authorizer),
			authz.WithInternalClientIDs(authConf.GetInternalClientIds()...),
		),
		ErrorMapping(errorMappings...),
		protovalidatemw.ProtoValidate(),
	}, nil
}

// newMetricsMiddleware 构造带 instrument 的指标中间件。
//
// 必须显式传入 counter 与 histogram：metrics.Server() 不带任何 Option 时
// 是彻底的空转——
//
//	if op.requests == nil && op.seconds == nil { return handler(ctx, req) }
//
// 中间件照样挂在链上、请求照样通过，但一个指标都不会产生。
// 这种失败没有任何报错，只有真去抓一次 /metrics 才看得出来。
//
// 构造失败时返回错误而不是降级跳过：静默跳过正是上面那个坑的翻版。
func newMetricsMiddleware() (middleware.Middleware, error) {
	meter := otel.Meter(meterName)

	requests, err := metrics.DefaultRequestsCounter(meter, metrics.DefaultServerRequestsCounterName)
	if err != nil {
		return nil, fmt.Errorf("server: 构造请求计数器: %w", err)
	}
	seconds, err := metrics.DefaultSecondsHistogram(meter, metrics.DefaultServerSecondsHistogramName)
	if err != nil {
		return nil, fmt.Errorf("server: 构造耗时直方图: %w", err)
	}

	return metrics.Server(
		metrics.WithRequests(requests),
		metrics.WithSeconds(seconds),
	), nil
}
