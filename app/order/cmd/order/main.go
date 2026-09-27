package main

import (
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
)

var Version string

func main() {
	platformruntime.Run(platformruntime.Spec{
		Name: "eagle.order", Version: Version, Build: buildApp,
		Requirements: config.Requirements{Database: true, Auth: true, HTTP: true, GRPC: true, ProductUpstream: true, RabbitMQ: true, ServiceAuth: true},
	})
}
