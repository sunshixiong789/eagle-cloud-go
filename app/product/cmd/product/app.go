package main

import (
	"log/slog"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	productdomain "github.com/eagle-go/eagle/app/product/internal/product/domain"
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

func productErrorMappings() []server.ErrorMappingRule {
	return []server.ErrorMappingRule{
		server.NotFound(productdomain.ErrProductNotFound, productv1.ErrorReason_ERROR_REASON_PRODUCT_NOT_FOUND),
		server.Conflict(productdomain.ErrProductSKUDuplicated, productv1.ErrorReason_ERROR_REASON_PRODUCT_SKU_DUPLICATED),
		server.BadRequest(productdomain.ErrInvalidProduct, productv1.ErrorReason_ERROR_REASON_INVALID_PRODUCT),
	}
}
