package main

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"

	orderv1 "github.com/eagle-go/eagle/api/eagle/order/v1"
	orderservice "github.com/eagle-go/eagle/app/order/internal/order/service"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func newGRPCServer(c *config.Server, ms []middleware.Middleware, svc *orderservice.OrderService) *grpc.Server {
	return server.NewGRPCServer(c, ms, nil, func(s *grpc.Server) {
		orderv1.RegisterOrderServiceServer(s, svc)
	})
}

func newHTTPServer(c *config.Server, ms []middleware.Middleware, svc *orderservice.OrderService) *http.Server {
	return server.NewHTTPServer(c, ms, nil, func(s *http.Server) {
		orderv1.RegisterOrderServiceHTTPServer(s, svc)
	})
}
