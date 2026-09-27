package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	kratosconfig "github.com/go-kratos/kratos/v3/config"
	configenv "github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"

	appconfig "github.com/eagle-go/eagle/pkg/platform/config"
)

// 配置文件里的字段名拼错不会导致编译失败——proto 解析时对不上的键
// 会被静默丢弃，直到运行时发现某个功能"没配置"才暴露。
// 这些测试真实加载服务的 configs/config.yaml，把这类错误提前到 CI。
func loadConfig(t *testing.T, service string) *appconfig.Bootstrap {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "app", service, "configs"))
	if err != nil {
		t.Fatalf("解析配置目录: %v", err)
	}

	c := kratosconfig.New(
		kratosconfig.WithSource(file.NewSource(path)),
		kratosconfig.WithResolveActualTypes(true),
	)
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Load(); err != nil {
		t.Fatalf("加载配置: %v", err)
	}

	var bc appconfig.Bootstrap
	if err := c.Scan(&bc); err != nil {
		t.Fatalf("解析配置到 proto: %v", err)
	}
	return &bc
}

func TestEnvironmentOverridesSensitiveDefaults(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "app", "admin", "configs"))
	if err != nil {
		t.Fatalf("解析配置目录: %v", err)
	}
	t.Setenv("EAGLE_DATABASE_DSN", "postgres://runtime:secret@db.internal:5432/eagle?sslmode=require")
	t.Setenv("EAGLE_SERVER_HTTP_ADDR", "0.0.0.0:18000")
	t.Setenv("EAGLE_AUTH_INTERNAL_CLIENT_ID", "eagle-product-worker")
	t.Setenv("EAGLE_MESSAGING_RABBITMQ_URL", "amqps://rabbit.internal/eagle")
	t.Setenv("EAGLE_OBSERVABILITY_TRACE_SAMPLE_RATIO", "0.05")

	c := kratosconfig.New(
		kratosconfig.WithSource(file.NewSource(path), configenv.NewSource("EAGLE")),
		kratosconfig.WithResolveActualTypes(true),
	)
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Load(); err != nil {
		t.Fatalf("加载配置: %v", err)
	}
	var bc appconfig.Bootstrap
	if err := c.Scan(&bc); err != nil {
		t.Fatalf("解析配置: %v", err)
	}
	if got := bc.GetData().GetDatabase().GetDsn(); !strings.Contains(got, "runtime:secret@db.internal") {
		t.Fatalf("DATABASE_DSN override not applied: %q", got)
	}
	if got := bc.GetServer().GetHttp().GetAddr(); got != "0.0.0.0:18000" {
		t.Fatalf("SERVER_HTTP_ADDR override not applied: %q", got)
	}
	if got := bc.GetAuth().GetInternalClientIds(); len(got) != 1 || got[0] != "eagle-product-worker" {
		t.Fatalf("AUTH_INTERNAL_CLIENT_ID override not applied: %v", got)
	}
	if got := bc.GetMessaging().GetRabbitmq().GetUrl(); got != "amqps://rabbit.internal/eagle" {
		t.Fatalf("MESSAGING_RABBITMQ_URL override not applied: %q", got)
	}
	if got := bc.GetObservability().GetTraceSampleRatio(); got != 0.05 {
		t.Fatalf("OBSERVABILITY_TRACE_SAMPLE_RATIO override not applied: %v", got)
	}
}

func TestConfigParses(t *testing.T) {
	tests := []struct {
		service      string
		requirements appconfig.Requirements
		httpAddr     string
		grpcAddr     string
		metricsAddr  string
		database     string
	}{
		{service: "admin", requirements: appconfig.Requirements{Database: true, Auth: true, HTTP: true, GRPC: true, File: true, RabbitMQ: true}, httpAddr: "0.0.0.0:8001", grpcAddr: "0.0.0.0:9001", metricsAddr: "0.0.0.0:9101", database: "eagle_admin"},
		{service: "product", requirements: appconfig.Requirements{Database: true, Auth: true, HTTP: true, GRPC: true, AuthorizationUpstream: true, Redis: true, ServiceAuth: true}, httpAddr: "0.0.0.0:8002", grpcAddr: "0.0.0.0:9002", metricsAddr: "0.0.0.0:9102", database: "eagle_product"},
		{service: "order", requirements: appconfig.Requirements{Database: true, Auth: true, HTTP: true, GRPC: true, ProductUpstream: true, RabbitMQ: true, ServiceAuth: true}, httpAddr: "0.0.0.0:8003", grpcAddr: "0.0.0.0:9003", metricsAddr: "0.0.0.0:9103", database: "eagle_order"},
	}
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			bc := loadConfig(t, tt.service)
			if err := appconfig.Validate(bc, tt.requirements); err != nil {
				t.Fatalf("配置校验失败: %v", err)
			}
			if got := bc.GetServer().GetHttp().GetAddr(); got != tt.httpAddr {
				t.Errorf("server.http.addr = %q, want %q", got, tt.httpAddr)
			}
			if got := bc.GetServer().GetGrpc().GetAddr(); got != tt.grpcAddr {
				t.Errorf("server.grpc.addr = %q, want %q", got, tt.grpcAddr)
			}
			if got := bc.GetObservability().GetMetricsAddr(); got != tt.metricsAddr {
				t.Errorf("observability.metrics_addr = %q, want %q", got, tt.metricsAddr)
			}
			if got := bc.GetData().GetDatabase().GetDsn(); !strings.Contains(got, "/"+tt.database+"?") {
				t.Errorf("data.database.dsn = %q, want database %q", got, tt.database)
			}

			switch tt.service {
			case "admin":
				if bc.GetFile().GetLocalDir() == "" || bc.GetFile().GetMaxSizeBytes() <= 0 {
					t.Error("admin file 配置未解析出来")
				}
				if bc.GetUpstream().GetAuthorizationEndpoint() != "" || bc.GetUpstream().GetProductEndpoint() != "" {
					t.Error("admin 不应携带 upstream 配置")
				}
			case "product":
				if bc.GetFile().GetLocalDir() != "" || bc.GetUpstream().GetProductEndpoint() != "" {
					t.Error("product 不应携带 file 或 product upstream 配置")
				}
			case "order":
				if bc.GetFile().GetLocalDir() != "" || bc.GetUpstream().GetAuthorizationEndpoint() != "" {
					t.Error("order 不应携带 file 或 authorization upstream 配置")
				}
			}
		})
	}
}

func TestServiceRequirementsDoNotCoupleUnrelatedConfig(t *testing.T) {
	bc := loadConfig(t, "admin")
	bc.File = nil
	bc.Upstream = nil
	if err := appconfig.Validate(bc, appconfig.Requirements{}); err != nil {
		t.Fatalf("admin should not require file or upstream config: %v", err)
	}
	if err := appconfig.Validate(bc, appconfig.Requirements{File: true}); err == nil {
		t.Fatal("file service must reject missing file config")
	}
}

// issuer 必须与 Keycloak realm 的 iss 完全一致，否则所有 token
// 都会因 iss 不匹配被拒——而报错信息不会指向这里，很难定位。
func TestAuthConfigPointsAtKeycloakRealm(t *testing.T) {
	auth := loadConfig(t, "admin").GetAuth()

	issuer := auth.GetIssuer()
	if issuer == "" {
		t.Fatal("auth.issuer 未配置，token 验签无法进行")
	}
	if !strings.Contains(issuer, "/realms/") {
		t.Errorf("issuer = %q，Keycloak 的 issuer 形如 http(s)://host/realms/<realm>", issuer)
	}
	// 尾斜杠会让 iss 比对失败，且肉眼极难发现
	if strings.HasSuffix(issuer, "/") {
		t.Errorf("issuer = %q 带了尾斜杠，与 token 里的 iss 对不上", issuer)
	}

	if auth.GetClientId() == "" {
		t.Error("auth.client_id 未配置，取不到 resource_access 里的 client 角色")
	}
	if auth.GetSuperAdminRole() == "" {
		t.Error("auth.super_admin_role 未配置")
	}
}

// 采样率越界会被 otelx 收敛到 [0,1]，但配置文件本身不该写错。
func TestObservabilityConfigIsSane(t *testing.T) {
	o := loadConfig(t, "admin").GetObservability()

	if r := o.GetTraceSampleRatio(); r < 0 || r > 1 {
		t.Errorf("trace_sample_ratio = %v, 应在 [0,1] 区间", r)
	}
	if o.GetMetricsAddr() == "" {
		t.Error("metrics_addr 未配置，Prometheus 抓不到指标")
	}
	if o.GetOtlpEndpoint() != "" && !o.GetOtlpInsecure() {
		t.Error("本地无 TLS collector 配置应显式启用 otlp_insecure")
	}
	// 指标端点必须与业务端口分开：它不经过认证鉴权中间件，
	// 与业务共用端口就等于给业务服务开了个免鉴权的口子
	bc := loadConfig(t, "admin")
	if o.GetMetricsAddr() == bc.GetServer().GetHttp().GetAddr() {
		t.Error("metrics_addr 与业务 HTTP 端口相同，指标端点会绕过鉴权暴露业务接口")
	}
}
