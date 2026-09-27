package main

import (
	"log/slog"

	orderv1 "github.com/eagle-go/eagle/api/eagle/order/v1"
	orderdomain "github.com/eagle-go/eagle/app/order/internal/order/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

//go:generate go run github.com/google/wire/cmd/wire

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	components, cleanup, err := wireApp(bc, logger)
	if err != nil {
		return platformruntime.Components{}, err
	}
	components.Cleanup = cleanup
	return components, nil
}

func orderErrorMappings() []server.ErrorMappingRule {
	return []server.ErrorMappingRule{
		server.NotFound(orderdomain.ErrOrderNotFound, orderv1.ErrorReason_ERROR_REASON_ORDER_NOT_FOUND),
		server.BadRequest(orderdomain.ErrProductUnavailable, orderv1.ErrorReason_ERROR_REASON_PRODUCT_UNAVAILABLE),
		server.BadRequest(orderdomain.ErrInvalidOrder, orderv1.ErrorReason_ERROR_REASON_INVALID_ORDER),
	}
}
