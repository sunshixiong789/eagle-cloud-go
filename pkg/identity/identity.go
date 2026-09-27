// Package identity 用 context.Context 承载已认证主体。
// 认证中间件写入，业务与鉴权中间件只从这里读取。
package identity

import (
	"context"
	"strings"
)

const (
	realmRolePrefix  = "realm:"
	clientRolePrefix = "client:"
)

func RealmRoleKey(role string) string { return realmRolePrefix + role }

func ClientRoleKey(clientID, role string) string {
	return clientRolePrefix + clientID + ":" + role
}

func ValidRoleKey(key string) bool {
	switch {
	case strings.HasPrefix(key, realmRolePrefix):
		role := strings.TrimPrefix(key, realmRolePrefix)
		return role != "" && !strings.Contains(role, ":")
	case strings.HasPrefix(key, clientRolePrefix):
		parts := strings.Split(strings.TrimPrefix(key, clientRolePrefix), ":")
		return len(parts) == 2 && parts[0] != "" && parts[1] != ""
	default:
		return false
	}
}

type ctxKey struct{}

// Principal 是一次调用的已认证主体，由认证中间件从 Keycloak token 构造。
//
// 它可能是终端用户，也可能是服务自身（client_credentials 服务账号），
// 由 IsService 区分。
type Principal struct {
	// Subject 是 Keycloak token 的 sub，用户在本系统的唯一锚点。
	//
	// 注意它是 UUID 字符串而不是自增整数：用户主数据在 Keycloak。
	Subject string

	// Username 是 preferred_username。用于日志与审计，不参与鉴权判定。
	Username string
	Email    string

	// ClientID 是换取该 token 的客户端（Keycloak 的 azp）。
	ClientID string

	// Scopes 是 token 携带的 scope。
	Scopes []string

	// Roles 汇总了带命名空间的 realm 角色与本服务 client 角色。
	// 普通 Casbin 策略判定基于它。
	Roles []string

	// ClientRoles 只包含本资源服务器在 resource_access 下的 client 角色。
	// 高影响的管理员短路必须基于它，防止 realm 级同名角色跨服务扩权。
	ClientRoles []string

	// IsService 标记这是服务账号令牌，背后没有真实用户。
	IsService bool
}

// HasClientRole 判断主体是否拥有本资源服务器命名空间内的角色。
func (p *Principal) HasClientRole(role string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.ClientRoles {
		if r == role {
			return true
		}
	}
	return false
}

// NewContext 把主体放入 context。仅认证中间件应调用。
func NewContext(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext 取出主体。未认证时 ok 为 false。
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok && p != nil
}

// Subject returns the authenticated subject or an empty string when the
// context is unauthenticated. Protected handlers normally receive a value.
func Subject(ctx context.Context) string {
	p, ok := FromContext(ctx)
	if !ok {
		return ""
	}
	return p.Subject
}
