// Package authz 提供方法级鉴权中间件。
//
// 权限要求声明在 proto 上（eagle.annotations.v1.perm），中间件在运行时
// 从方法描述符读出并交给 Casbin 判定，业务 handler 里不出现任何鉴权代码。
//
// 职责边界：
//   - Keycloak 负责「你是谁、你有哪些角色」，角色随 token 下发
//   - Casbin 负责「这个角色能不能调这个接口」，策略存在本库
//
// 判定顺序：
//  1. public 方法直接放行
//  2. 无主体 -> 401
//  3. 未声明权限码 -> 已登录即可
//  4. 本服务命名空间内的超管 client 角色 -> 短路放行
//  5. 其余 -> 交 Casbin 按角色判定
package authz

import (
	"context"
	"slices"

	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"

	annotationsv1 "github.com/eagle-go/eagle/api/eagle/annotations/v1"
	"github.com/eagle-go/eagle/pkg/identity"
)

// 错误 reason，客户端据此做分支处理。
const (
	ReasonUnauthenticated = "UNAUTHENTICATED"
	ReasonForbidden       = "FORBIDDEN"
)

type options struct {
	authorizer        Authorizer
	superAdminRole    string
	internalClientIDs []string
}

// Authorizer is the narrow policy decision port used by the middleware. The
// admin service supplies its database-backed Casbin enforcer; resource services
// supply a local enforcer refreshed from versioned access-service snapshots.
type Authorizer interface {
	AllowContext(context.Context, []string, string) (bool, error)
}

// Option 配置鉴权中间件。
type Option func(*options)

// WithEnforcer 注入 Casbin 判定器。
// 不设置时，所有声明了权限码的方法都会被拒绝（fail closed）。
func WithEnforcer(e *Enforcer) Option {
	return WithAuthorizer(e)
}

// WithAuthorizer injects either a local or remote policy decision point.
func WithAuthorizer(a Authorizer) Option {
	return func(o *options) { o.authorizer = a }
}

// WithSuperAdminRole 设置超管角色，具备该角色的主体跳过 Casbin 判定。
func WithSuperAdminRole(role string) Option {
	return func(o *options) { o.superAdminRole = role }
}

// WithInternalClientIDs sets the Keycloak clients allowed to invoke INTERNAL RPCs.
func WithInternalClientIDs(clientIDs ...string) Option {
	return func(o *options) { o.internalClientIDs = slices.Clone(clientIDs) }
}

// Server 返回鉴权中间件。
//
// 必须挂在认证中间件之后——后者负责验签 Keycloak token 并把
// identity.Principal 放进 context，这里只做授权判定。
func Server(opts ...Option) middleware.Middleware {
	o := &options{superAdminRole: "admin"}
	for _, opt := range opts {
		opt(o)
	}

	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			tr, ok := transport.FromServerContext(ctx)
			if !ok {
				// 拿不到传输层信息说明中间件被挂在了非服务端链路上，
				// 无法判定，按拒绝处理
				return nil, errors.Forbidden(ReasonForbidden, "无法解析调用上下文")
			}

			policy := PolicyFor(tr.Operation())
			if !policy.Known || policy.Access == annotationsv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED {
				return nil, errors.Forbidden(ReasonForbidden, "RPC 未声明有效的访问策略")
			}
			if policy.Access == annotationsv1.AccessLevel_ACCESS_LEVEL_PUBLIC {
				return handler(ctx, req)
			}

			p, authenticated := identity.FromContext(ctx)
			if !authenticated {
				return nil, errors.Unauthorized(ReasonUnauthenticated, "未登录或凭证已失效")
			}

			if policy.Access == annotationsv1.AccessLevel_ACCESS_LEVEL_AUTHENTICATED {
				return handler(ctx, req)
			}
			if policy.Access == annotationsv1.AccessLevel_ACCESS_LEVEL_INTERNAL {
				if !p.IsService || !slices.Contains(o.internalClientIDs, p.ClientID) {
					return nil, errors.Forbidden(ReasonForbidden, "仅允许受信任的服务账号调用")
				}
				return handler(ctx, req)
			}
			if policy.Access != annotationsv1.AccessLevel_ACCESS_LEVEL_PERMISSION_REQUIRED || policy.Perm == "" {
				return nil, errors.Forbidden(ReasonForbidden, "RPC 访问策略配置错误")
			}

			if err := o.check(ctx, p, policy.Perm); err != nil {
				return nil, err
			}
			return handler(ctx, req)
		}
	}
}

func (o *options) check(ctx context.Context, p *identity.Principal, perm string) error {
	if o.superAdminRole != "" && p.HasClientRole(o.superAdminRole) {
		recordDecision(ctx, "super_admin_bypass", perm)
		return nil
	}

	if o.authorizer == nil {
		// 没配 enforcer 却要求权限码，属于装配错误。
		// 这里拒绝而不是放行——鉴权组件的失败方向必须是关闭的。
		return errors.Forbidden(ReasonForbidden, "鉴权未正确装配：缺少判定器")
	}

	// 服务账号与终端用户走同一套判定：两者的角色都由 Keycloak 下发，
	// 在 Casbin 眼里没有区别。为服务账号单开一条 scope 判定分支
	// 会形成第二套授权语义，日后必然出现两边配置不一致。
	allowed, err := o.authorizer.AllowContext(ctx, p.Roles, perm)
	if err != nil {
		// 判定失败时拒绝。放行会让策略存储抖动直接变成越权。
		return errors.Forbidden(ReasonForbidden, "权限校验失败")
	}
	if !allowed {
		return errors.Forbidden(ReasonForbidden, "缺少权限: "+perm)
	}
	return nil
}
