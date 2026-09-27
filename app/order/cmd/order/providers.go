package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/wire"

	orderv1 "github.com/eagle-go/eagle/api/eagle/order/v1"
	orderapp "github.com/eagle-go/eagle/app/order/internal/order/application"
	orderdomain "github.com/eagle-go/eagle/app/order/internal/order/domain"
	orderinfra "github.com/eagle-go/eagle/app/order/internal/order/infrastructure"
	orderservice "github.com/eagle-go/eagle/app/order/internal/order/service"
	platformdb "github.com/eagle-go/eagle/app/order/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

type outboxRelay struct{}

var providerSet = wire.NewSet(
	provideData,
	provideAuth,
	provideServer,
	provideUpstream,
	provideServiceAuth,
	provideRabbitMQ,
	platformdb.Open,
	orderinfra.NewProductClient,
	provideProductCatalog,
	orderinfra.NewRepository,
	provideOrderWriter,
	provideOrderReader,
	orderapp.NewUsecase,
	orderservice.NewOrderService,
	providePublisher,
	provideOutboxRelay,
	provideAuthorizer,
	server.NewVerifier,
	provideMiddlewares,
	provideGRPCServer,
	provideHTTPServer,
	newComponents,
)

// 默认构建不编译 wire.go，保留对 providerSet 的引用以免被标成未使用。
var _ = providerSet

func provideData(bc *config.Bootstrap) *config.Data               { return bc.GetData() }
func provideAuth(bc *config.Bootstrap) *config.Auth               { return bc.GetAuth() }
func provideServer(bc *config.Bootstrap) *config.Server           { return bc.GetServer() }
func provideUpstream(bc *config.Bootstrap) *config.Upstream       { return bc.GetUpstream() }
func provideServiceAuth(bc *config.Bootstrap) *config.ServiceAuth { return bc.GetServiceAuth() }
func provideRabbitMQ(bc *config.Bootstrap) *config.Messaging_RabbitMQ {
	return bc.GetMessaging().GetRabbitmq()
}

func provideProductCatalog(client *orderinfra.ProductClient) orderdomain.ProductCatalog {
	return client
}

func provideOrderReader(repo orderdomain.Repository) orderdomain.Reader { return repo }
func provideOrderWriter(repo orderdomain.Repository) orderdomain.Writer { return repo }

func provideAuthorizer() authz.Authorizer {
	return nil
}

func providePublisher(c *config.Messaging_RabbitMQ) (*rabbitmq.Publisher, func(), error) {
	publisher, err := rabbitmq.NewPublisher(rabbitmq.Config{
		URL: c.GetUrl(), Exchange: c.GetExchange(),
		ReconnectBackoff: c.GetReconnectBackoff().AsDuration(),
	})
	if err != nil {
		return nil, nil, err
	}
	return publisher, publisher.Close, nil
}

func provideOutboxRelay(db *platformdb.Database, publisher *rabbitmq.Publisher, logger *slog.Logger) (outboxRelay, func(), error) {
	return outboxRelay{}, orderinfra.NewOutboxRelay(db, publisher, logger), nil
}

func provideMiddlewares(
	logger *slog.Logger,
	verifier *authn.Verifier,
	authorizer authz.Authorizer,
	auth *config.Auth,
) ([]middleware.Middleware, error) {
	return server.NewMiddlewares(logger, verifier, authorizer, auth, orderErrorMappings()...)
}

func provideGRPCServer(c *config.Server, ms []middleware.Middleware, svc *orderservice.OrderService) *grpc.Server {
	return server.NewGRPCServer(c, ms, func(s *grpc.Server) {
		orderv1.RegisterOrderServiceServer(s, svc)
	})
}

func provideHTTPServer(c *config.Server, ms []middleware.Middleware, svc *orderservice.OrderService) *http.Server {
	return server.NewHTTPServer(c, ms, nil, func(s *http.Server) {
		orderv1.RegisterOrderServiceHTTPServer(s, svc)
	})
}

func newComponents(gs *grpc.Server, hs *http.Server, _ outboxRelay) platformruntime.Components {
	return platformruntime.Components{Servers: []transport.Server{gs, hs}}
}
