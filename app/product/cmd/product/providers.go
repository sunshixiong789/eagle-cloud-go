package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/wire"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	platformdb "github.com/eagle-go/eagle/app/product/internal/platform/database"
	productapp "github.com/eagle-go/eagle/app/product/internal/product/application"
	productdomain "github.com/eagle-go/eagle/app/product/internal/product/domain"
	productinfra "github.com/eagle-go/eagle/app/product/internal/product/infrastructure"
	accessclient "github.com/eagle-go/eagle/app/product/internal/product/infrastructure/accessclient"
	productservice "github.com/eagle-go/eagle/app/product/internal/product/service"
	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

var providerSet = wire.NewSet(
	provideData,
	provideAuth,
	provideServer,
	provideUpstream,
	provideServiceAuth,
	provideRedis,
	platformdb.Open,
	accessclient.NewAuthorizer,
	provideAuthorizer,
	productinfra.NewRedisProductCache,
	provideRepository,
	provideProductReader,
	productapp.NewCommands,
	productservice.NewProductService,
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
func provideRedis(bc *config.Bootstrap) *config.Cache_Redis {
	return bc.GetCache().GetRedis()
}

func provideAuthorizer(authorizer *accessclient.Authorizer) authz.Authorizer {
	return authorizer
}

func provideRepository(db *platformdb.Database, cache *productinfra.RedisProductCache, logger *slog.Logger) productdomain.Repository {
	return productinfra.NewCachedRepository(productinfra.NewRepository(db), cache, logger)
}

func provideProductReader(repo productdomain.Repository) productdomain.Reader { return repo }

func provideMiddlewares(
	logger *slog.Logger,
	verifier *authn.Verifier,
	authorizer authz.Authorizer,
	auth *config.Auth,
) ([]middleware.Middleware, error) {
	return server.NewMiddlewares(logger, verifier, authorizer, auth, productErrorMappings()...)
}

func provideGRPCServer(c *config.Server, ms []middleware.Middleware, svc *productservice.ProductService) *grpc.Server {
	return server.NewGRPCServer(c, ms, func(s *grpc.Server) {
		productv1.RegisterProductServiceServer(s, svc)
	})
}

func provideHTTPServer(c *config.Server, ms []middleware.Middleware, svc *productservice.ProductService) *http.Server {
	return server.NewHTTPServer(c, ms, nil, func(s *http.Server) {
		productv1.RegisterProductServiceHTTPServer(s, svc)
	})
}

func newComponents(gs *grpc.Server, hs *http.Server) platformruntime.Components {
	return platformruntime.Components{Servers: []transport.Server{gs, hs}}
}
