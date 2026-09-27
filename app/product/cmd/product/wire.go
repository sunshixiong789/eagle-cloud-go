//go:build wireinject

package main

import (
	"log/slog"

	"github.com/google/wire"

	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
)

func wireApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, func(), error) {
	wire.Build(providerSet)
	return platformruntime.Components{}, nil, nil
}
