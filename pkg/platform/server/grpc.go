package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/grpc"

	"github.com/eagle-go/eagle/pkg/platform/config"
)

// GRPCRegistrar lets each process register only the APIs it owns.
type GRPCRegistrar func(*grpc.Server)

func NewGRPCServer(c *config.Server, ms []middleware.Middleware, registrars ...GRPCRegistrar) *grpc.Server {
	opts := []grpc.ServerOption{grpc.Middleware(ms...)}
	if n := c.GetGrpc().GetNetwork(); n != "" {
		opts = append(opts, grpc.Network(n))
	}
	if addr := c.GetGrpc().GetAddr(); addr != "" {
		opts = append(opts, grpc.Address(addr))
	}
	if t := c.GetGrpc().GetTimeout(); t != nil {
		opts = append(opts, grpc.Timeout(t.AsDuration()))
	}

	srv := grpc.NewServer(opts...)
	for _, register := range registrars {
		register(srv)
	}
	return srv
}
