package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/transport"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	platformdb "github.com/eagle-go/eagle/app/product/internal/platform/database"
	productapp "github.com/eagle-go/eagle/app/product/internal/product/application"
	productdomain "github.com/eagle-go/eagle/app/product/internal/product/domain"
	productinfra "github.com/eagle-go/eagle/app/product/internal/product/infrastructure"
	accessclient "github.com/eagle-go/eagle/app/product/internal/product/infrastructure/accessclient"
	productservice "github.com/eagle-go/eagle/app/product/internal/product/service"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	authorizer, closeAuthorizer, err := accessclient.NewAuthorizer(bc.GetUpstream(), bc.GetServiceAuth(), logger)
	if err != nil {
		return platformruntime.Components{}, err
	}
	ms, err := server.NewMiddlewares(logger, server.NewVerifier(bc.GetAuth()), authorizer, bc.GetAuth(), productErrorMappings()...)
	if err != nil {
		closeAuthorizer()
		return platformruntime.Components{}, err
	}
	db, closeDB, err := platformdb.Open(bc.GetData())
	if err != nil {
		closeAuthorizer()
		return platformruntime.Components{}, err
	}
	cache, closeCache, err := productinfra.NewRedisProductCache(bc.GetCache().GetRedis())
	if err != nil {
		closeDB()
		closeAuthorizer()
		return platformruntime.Components{}, err
	}

	repo := productinfra.NewCachedRepository(productinfra.NewRepository(db), cache, logger)
	svc := productservice.NewProductService(productapp.NewCommands(repo), repo)
	gs := newGRPCServer(bc.GetServer(), ms, svc)
	hs := newHTTPServer(bc.GetServer(), ms, svc)
	return platformruntime.Components{
		Servers: []transport.Server{gs, hs},
		Cleanup: func() {
			closeCache()
			closeDB()
			closeAuthorizer()
		},
	}, nil
}

func productErrorMappings() []server.ErrorMappingRule {
	return []server.ErrorMappingRule{
		server.NotFound(productdomain.ErrProductNotFound, productv1.ErrorReason_ERROR_REASON_PRODUCT_NOT_FOUND),
		server.Conflict(productdomain.ErrProductSKUDuplicated, productv1.ErrorReason_ERROR_REASON_PRODUCT_SKU_DUPLICATED),
		server.BadRequest(productdomain.ErrInvalidProduct, productv1.ErrorReason_ERROR_REASON_INVALID_PRODUCT),
	}
}
