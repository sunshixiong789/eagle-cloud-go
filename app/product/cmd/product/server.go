package main

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	productservice "github.com/eagle-go/eagle/app/product/internal/product/service"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func newGRPCServer(c *config.Server, ms []middleware.Middleware, svc *productservice.ProductService) *grpc.Server {
	return server.NewGRPCServer(c, ms, func(s *grpc.Server) {
		productv1.RegisterProductServiceServer(s, svc)
	})
}

func newHTTPServer(c *config.Server, ms []middleware.Middleware, svc *productservice.ProductService) *http.Server {
	return server.NewHTTPServer(c, ms, nil, func(s *http.Server) {
		productv1.RegisterProductServiceHTTPServer(s, svc)
	})
}
