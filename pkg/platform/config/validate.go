package config

import (
	"errors"
	"net/url"
	"strings"
)

type Requirements struct {
	Database              bool
	Auth                  bool
	HTTP                  bool
	GRPC                  bool
	File                  bool
	AuthorizationUpstream bool
	ProductUpstream       bool
	Redis                 bool
	RabbitMQ              bool
	ServiceAuth           bool
}

// Validate 校验部署时最终合并出的配置。requirements 为空时校验示例配置
// 的全部字段；服务进程只声明自己真实使用的可选配置。
func Validate(b *Bootstrap, requirements ...Requirements) error {
	if b == nil {
		return errors.New("config: bootstrap is nil")
	}
	var errs []error
	db := b.GetData().GetDatabase()
	auth := b.GetAuth()
	server := b.GetServer()
	obs := b.GetObservability()
	file := b.GetFile()
	upstream := b.GetUpstream()
	redis := b.GetCache().GetRedis()
	rabbit := b.GetMessaging().GetRabbitmq()
	serviceAuth := b.GetServiceAuth()
	required := Requirements{Database: true, Auth: true, HTTP: true, GRPC: true}
	if len(requirements) == 0 {
		required.File = true
		required.AuthorizationUpstream = true
		required.ProductUpstream = true
		required.Redis = true
		required.RabbitMQ = true
		required.ServiceAuth = true
	} else {
		required = requirements[0]
	}

	if required.Database && db.GetDsn() == "" {
		errs = append(errs, errors.New("data.database.dsn is required"))
	} else if required.Database {
		if u, err := url.Parse(db.GetDsn()); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			errs = append(errs, errors.New("data.database.dsn must be a postgres URL"))
		}
	}
	if required.Database && (db.GetMaxConns() <= 0 || db.GetMaxIdleConns() < 0 || db.GetMaxIdleConns() > db.GetMaxConns()) {
		errs = append(errs, errors.New("database pool requires 0 <= max_idle_conns <= max_conns and max_conns > 0"))
	}
	if required.Auth {
		if err := validateIssuer(auth.GetIssuer()); err != nil {
			errs = append(errs, err)
		}
		if auth.GetClientId() == "" || auth.GetAudience() == "" {
			errs = append(errs, errors.New("auth.client_id and auth.audience are required"))
		}
		if auth.GetSuperAdminRole() == "" {
			errs = append(errs, errors.New("auth.super_admin_role is required"))
		}
		if len(auth.GetInternalClientIds()) == 0 {
			errs = append(errs, errors.New("auth.internal_client_ids must not be empty"))
		}
	}
	if required.HTTP && server.GetHttp().GetAddr() == "" {
		errs = append(errs, errors.New("server.http.addr is required"))
	}
	if required.GRPC && server.GetGrpc().GetAddr() == "" {
		errs = append(errs, errors.New("server.grpc.addr is required"))
	}
	if obs.GetTraceSampleRatio() < 0 || obs.GetTraceSampleRatio() > 1 {
		errs = append(errs, errors.New("observability.trace_sample_ratio must be in [0,1]"))
	}
	if obs.GetMetricsAddr() != "" && obs.GetMetricsAddr() == server.GetHttp().GetAddr() {
		errs = append(errs, errors.New("observability.metrics_addr must differ from server.http.addr"))
	}
	if required.File && file.GetMaxSizeBytes() <= 0 {
		errs = append(errs, errors.New("file.max_size_bytes must be positive"))
	}
	if required.File {
		switch file.GetProvider() {
		case "local":
			if strings.TrimSpace(file.GetLocalDir()) == "" {
				errs = append(errs, errors.New("file.local_dir is required for local provider"))
			}
		case "s3":
			s3 := file.GetS3()
			if strings.TrimSpace(s3.GetEndpoint()) == "" || strings.TrimSpace(s3.GetBucket()) == "" ||
				strings.TrimSpace(s3.GetAccessKey()) == "" || strings.TrimSpace(s3.GetSecretKey()) == "" {
				errs = append(errs, errors.New("file.s3 endpoint, bucket, access_key and secret_key are required"))
			}
		default:
			errs = append(errs, errors.New("file.provider must be local or s3"))
		}
	}
	if required.AuthorizationUpstream && strings.TrimSpace(upstream.GetAuthorizationEndpoint()) == "" {
		errs = append(errs, errors.New("upstream.authorization_endpoint is required"))
	}
	if required.ProductUpstream && strings.TrimSpace(upstream.GetProductEndpoint()) == "" {
		errs = append(errs, errors.New("upstream.product_endpoint is required"))
	}
	if (required.AuthorizationUpstream || required.ProductUpstream) && (upstream.GetTimeout() == nil || upstream.GetTimeout().AsDuration() <= 0) {
		errs = append(errs, errors.New("upstream.timeout must be positive"))
	}
	if required.AuthorizationUpstream || required.ProductUpstream {
		if upstream.GetMaxAttempts() < 1 || upstream.GetMaxAttempts() > 5 {
			errs = append(errs, errors.New("upstream.max_attempts must be in [1,5]"))
		}
		if upstream.GetRetryBackoff() == nil || upstream.GetRetryBackoff().AsDuration() <= 0 {
			errs = append(errs, errors.New("upstream.retry_backoff must be positive"))
		}
	}
	if required.AuthorizationUpstream && (upstream.GetAuthorizationRefreshInterval() == nil || upstream.GetAuthorizationRefreshInterval().AsDuration() <= 0) {
		errs = append(errs, errors.New("upstream.authorization_refresh_interval must be positive"))
	}
	if required.Redis {
		if !redis.GetEnabled() || strings.TrimSpace(redis.GetAddress()) == "" {
			errs = append(errs, errors.New("cache.redis must be enabled and address is required"))
		}
		if redis.GetTtl() == nil || redis.GetTtl().AsDuration() <= 0 {
			errs = append(errs, errors.New("cache.redis.ttl must be positive"))
		}
	}
	if required.RabbitMQ {
		if !rabbit.GetEnabled() || strings.TrimSpace(rabbit.GetUrl()) == "" || strings.TrimSpace(rabbit.GetExchange()) == "" ||
			strings.TrimSpace(rabbit.GetOrderCreatedQueue()) == "" {
			errs = append(errs, errors.New("messaging.rabbitmq must be enabled and url/exchange/order_created_queue are required"))
		} else if u, err := url.Parse(rabbit.GetUrl()); err != nil || (u.Scheme != "amqp" && u.Scheme != "amqps") || u.Host == "" {
			errs = append(errs, errors.New("messaging.rabbitmq.url must be an amqp or amqps URL"))
		}
		if rabbit.GetReconnectBackoff() == nil || rabbit.GetReconnectBackoff().AsDuration() <= 0 {
			errs = append(errs, errors.New("messaging.rabbitmq.reconnect_backoff must be positive"))
		}
		if rabbit.GetConsumerMaxAttempts() < 1 || rabbit.GetConsumerMaxAttempts() > 10 {
			errs = append(errs, errors.New("messaging.rabbitmq.consumer_max_attempts must be in [1,10]"))
		}
		if rabbit.GetConsumerRetryBackoff() == nil || rabbit.GetConsumerRetryBackoff().AsDuration() <= 0 {
			errs = append(errs, errors.New("messaging.rabbitmq.consumer_retry_backoff must be positive"))
		}
	}
	if required.ServiceAuth && (strings.TrimSpace(serviceAuth.GetTokenUrl()) == "" ||
		strings.TrimSpace(serviceAuth.GetClientId()) == "" || strings.TrimSpace(serviceAuth.GetClientSecret()) == "") {
		errs = append(errs, errors.New("service_auth token_url, client_id and client_secret are required"))
	}
	return errors.Join(errs...)
}

func validateIssuer(raw string) error {
	if raw == "" {
		return errors.New("auth.issuer is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || !strings.Contains(u.Path, "/realms/") {
		return errors.New("auth.issuer must be an absolute Keycloak realm URL")
	}
	if strings.HasSuffix(raw, "/") {
		return errors.New("auth.issuer must not have a trailing slash")
	}
	return nil
}
