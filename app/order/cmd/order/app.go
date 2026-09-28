package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/transport"

	orderv1 "github.com/eagle-go/eagle/api/eagle/order/v1"
	orderapp "github.com/eagle-go/eagle/app/order/internal/order/application"
	orderdomain "github.com/eagle-go/eagle/app/order/internal/order/domain"
	orderinfra "github.com/eagle-go/eagle/app/order/internal/order/infrastructure"
	orderservice "github.com/eagle-go/eagle/app/order/internal/order/service"
	platformdb "github.com/eagle-go/eagle/app/order/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	ms, err := server.NewMiddlewares(logger, server.NewVerifier(bc.GetAuth()), nil, bc.GetAuth(), orderErrorMappings()...)
	if err != nil {
		return platformruntime.Components{}, err
	}
	db, closeDB, err := platformdb.Open(bc.GetData())
	if err != nil {
		return platformruntime.Components{}, err
	}
	products, closeProducts, err := orderinfra.NewProductClient(bc.GetUpstream(), bc.GetServiceAuth())
	if err != nil {
		closeDB()
		return platformruntime.Components{}, err
	}
	c := bc.GetMessaging().GetRabbitmq()
	publisher, err := rabbitmq.NewPublisher(rabbitmq.Config{
		URL: c.GetUrl(), Exchange: c.GetExchange(),
		ReconnectBackoff: c.GetReconnectBackoff().AsDuration(),
	})
	if err != nil {
		closeProducts()
		closeDB()
		return platformruntime.Components{}, err
	}

	repo := orderinfra.NewRepository(db)
	svc := orderservice.NewOrderService(orderapp.NewUsecase(repo, products), repo)
	gs := newGRPCServer(bc.GetServer(), ms, svc)
	hs := newHTTPServer(bc.GetServer(), ms, svc)
	stopRelay := orderinfra.NewOutboxRelay(db, publisher, logger)
	return platformruntime.Components{
		Servers: []transport.Server{gs, hs},
		Cleanup: func() {
			stopRelay()
			publisher.Close()
			closeProducts()
			closeDB()
		},
	}, nil
}

func orderErrorMappings() []server.ErrorMappingRule {
	return []server.ErrorMappingRule{
		server.Conflict(orderdomain.ErrIdempotencyConflict, orderv1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT),
		server.NotFound(orderdomain.ErrOrderNotFound, orderv1.ErrorReason_ERROR_REASON_ORDER_NOT_FOUND),
		server.BadRequest(orderdomain.ErrProductUnavailable, orderv1.ErrorReason_ERROR_REASON_PRODUCT_UNAVAILABLE),
		server.BadRequest(orderdomain.ErrInvalidOrder, orderv1.ErrorReason_ERROR_REASON_INVALID_ORDER),
	}
}
