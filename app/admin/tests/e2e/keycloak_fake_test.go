package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeKeycloak 是一个最小的 OIDC 提供方替身：暴露 JWKS 端点，
// 并按 Keycloak 的 claim 结构签发真实的 RS256 token。
//
// 用它而不是直接构造 Principal，是因为需要验证的恰恰是「真实 token
// 经过验签、解析、映射之后能不能得到正确的 Principal」这条链路。
// 跳过签名和 JSON 解析去直接塞 Principal，等于把最容易出错的一段
// 排除在测试之外——claim 名写错、角色嵌套层级搞错都不会被发现。
//
// 它替代不了真实 Keycloak 的地方只有一处：token 的签发流程本身
// （登录、授权码交换、refresh）。token 的形状与验证路径是等价的。
type fakeKeycloak struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	keyID  string
}

func newFakeKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥: %v", err)
	}

	fk := &fakeKeycloak{key: key, keyID: "test-key-1"}

	mux := http.NewServeMux()
	// Keycloak 的 JWKS 路径不是 OIDC 常见的 /.well-known/jwks.json，
	// 这里刻意用真实路径，以验证 authn 的默认推导逻辑正确
	mux.HandleFunc("/protocol/openid-connect/certs", func(w http.ResponseWriter, _ *http.Request) {
		jwks := jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{{
				Key:       key.Public(),
				KeyID:     fk.keyID,
				Algorithm: string(jose.RS256),
				Use:       "sig",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	fk.server = httptest.NewServer(mux)
	t.Cleanup(fk.server.Close)
	return fk
}

// issuer 返回 realm 的 issuer。真实 Keycloak 形如
// https://host/realms/eagle，这里用 httptest 的地址充当。
func (fk *fakeKeycloak) issuer() string { return fk.server.URL }

// tokenOpts 描述要签发的 token。
type tokenOpts struct {
	subject   string
	username  string
	realmRole []string
	// clientRoles 是 resource_access 里按客户端划分的角色
	clientRoles map[string][]string
	audience    []string
	expiresIn   time.Duration
	notBefore   time.Duration
	algorithm   jose.SignatureAlgorithm
	// notSigned 为 true 时用另一把密钥签名，模拟伪造 token
	wrongKey bool
}

// mint 按 Keycloak 的 access token 结构签发 token。
func (fk *fakeKeycloak) mint(t *testing.T, o tokenOpts) string {
	t.Helper()

	if o.expiresIn == 0 {
		o.expiresIn = 15 * time.Minute
	}
	if o.audience == nil {
		o.audience = []string{clientID}
	}
	if o.algorithm == "" {
		o.algorithm = jose.RS256
	}

	signingKey := fk.key
	if o.wrongKey {
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成伪造密钥: %v", err)
		}
		signingKey = other
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: o.algorithm, Key: signingKey},
		// kid 必须带上：RemoteKeySet 按 kid 选公钥
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", fk.keyID),
	)
	if err != nil {
		t.Fatalf("构造签名器: %v", err)
	}

	now := time.Now()

	// 这里的 JSON tag 必须与 Keycloak 实际下发的字段名逐字一致——
	// 整个测试的价值就在于验证 pkg/authn 能正确解析这个结构
	claims := map[string]any{
		"iss":                fk.issuer(),
		"sub":                o.subject,
		"aud":                o.audience,
		"exp":                now.Add(o.expiresIn).Unix(),
		"iat":                now.Unix(),
		"typ":                "Bearer",
		"azp":                clientID,
		"scope":              "openid profile email",
		"preferred_username": o.username,
		"email":              o.username + "@example.com",
		"realm_access": map[string]any{
			"roles": o.realmRole,
		},
	}
	if o.notBefore != 0 {
		claims["nbf"] = now.Add(o.notBefore).Unix()
	}
	if len(o.clientRoles) > 0 {
		ra := make(map[string]any, len(o.clientRoles))
		for cid, roles := range o.clientRoles {
			ra[cid] = map[string]any{"roles": roles}
		}
		claims["resource_access"] = ra
	}

	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("签发 token: %v", err)
	}
	return raw
}

// userToken 签发一个普通用户 token。
func (fk *fakeKeycloak) userToken(t *testing.T, username string, roles ...string) string {
	t.Helper()
	return fk.mint(t, tokenOpts{
		subject:   "sub-" + username,
		username:  username,
		realmRole: roles,
	})
}

// serviceToken 签发一个 client_credentials 服务账号 token。
//
// Keycloak 下服务账号的 sub 是服务账号用户的 UUID，
// 用户名固定为 service-account-<clientId> —— 这是识别服务令牌的依据。
func (fk *fakeKeycloak) serviceToken(t *testing.T, clientName string, roles ...string) string {
	t.Helper()
	return fk.mint(t, tokenOpts{
		subject:   "uuid-of-service-account",
		username:  "service-account-" + clientName,
		realmRole: roles,
	})
}
