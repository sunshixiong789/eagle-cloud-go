package authn

import (
	"strings"

	"github.com/eagle-go/eagle/pkg/identity"
)

// serviceAccountPrefix 是 Keycloak 为 client_credentials 服务账号
// 生成的用户名前缀，形如 service-account-eagle-system。
// 这是 Keycloak 的既定约定，也是区分「服务令牌」与「用户令牌」最可靠的信号。
const serviceAccountPrefix = "service-account-"

// Claims 是 Keycloak 签发的 access token 载荷。
//
// 只声明本项目实际消费的字段。Keycloak 的 token 还带 session_state、
// allowed-origins、email_verified 等一堆字段，全声明进来会造成
// 「这些都参与判定」的错觉——鉴权结构体尤其要避免这种误导。
type Claims struct {
	Subject string `json:"sub"`
	// AuthorizedParty 是 Keycloak 的 azp，即换取该 token 的客户端。
	// 注意不是 client_id——Keycloak 的 access token 里用的是 azp。
	AuthorizedParty string `json:"azp"`

	// Scope 为空格分隔，遵循 RFC 6749
	Scope string `json:"scope"`

	Username string `json:"preferred_username"`
	Email    string `json:"email"`

	// RealmAccess 是 realm 级角色。Keycloak 把角色嵌在这里，
	// 而不是放在顶层 roles claim——直接读 roles 会永远拿到空。
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`

	// ResourceAccess 是各客户端的 client 级角色，键为 client id。
	ResourceAccess map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
}

// Scopes 把空格分隔的 scope 串拆成切片。
func (c *Claims) Scopes() []string {
	if c.Scope == "" {
		return nil
	}
	return strings.Fields(c.Scope)
}

// Roles 汇总 realm 角色与指定客户端的 client 角色。
//
// clientID 为空时只返回 realm 角色。Casbin 判定和超管短路都基于这个结果，
// 所以两级角色必须一起返回——只看其中一级会让在 Keycloak 里
// 按 client 授权的角色静默失效。
func (c *Claims) Roles(clientID string) []string {
	roles := make([]string, 0, len(c.RealmAccess.Roles))
	for _, role := range c.RealmAccess.Roles {
		if role != "" {
			roles = append(roles, identity.RealmRoleKey(role))
		}
	}

	if clientID != "" {
		if ra, ok := c.ResourceAccess[clientID]; ok {
			for _, role := range ra.Roles {
				if role != "" {
					roles = append(roles, identity.ClientRoleKey(clientID, role))
				}
			}
		}
	}
	return roles
}

// ClientRoles 只返回指定资源服务器命名空间内的角色。
// 超级管理员之类的高影响短路只能基于这份集合，避免同名 realm 角色
// 意外获得所有服务的全局管理能力。
func (c *Claims) ClientRoles(clientID string) []string {
	if clientID == "" {
		return nil
	}
	ra, ok := c.ResourceAccess[clientID]
	if !ok {
		return nil
	}
	return append([]string(nil), ra.Roles...)
}

// IsServiceToken 判断这是不是 client_credentials 签发的服务令牌。
//
// Keycloak 下不能用「sub == client_id」判断：服务账号有自己的用户 UUID，
// sub 是那个 UUID 而不是客户端标识。可靠信号是用户名的
// service-account- 前缀。
func (c *Claims) IsServiceToken() bool {
	return strings.HasPrefix(c.Username, serviceAccountPrefix)
}
