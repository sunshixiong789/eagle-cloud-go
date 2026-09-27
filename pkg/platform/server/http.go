package server

import (
	stdhttp "net/http"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/http"

	"github.com/eagle-go/eagle/pkg/platform/config"
)

// HTTPRegistrar lets each process register only the APIs it owns.
type HTTPRegistrar func(*http.Server)

func NewHTTPServer(
	c *config.Server,
	ms []middleware.Middleware,
	filters []http.FilterFunc,
	registrars ...HTTPRegistrar,
) *http.Server {
	opts := []http.ServerOption{http.Middleware(ms...)}
	if len(filters) > 0 {
		opts = append(opts, http.Filter(filters...))
	}
	if n := c.GetHttp().GetNetwork(); n != "" {
		opts = append(opts, http.Network(n))
	}
	if addr := c.GetHttp().GetAddr(); addr != "" {
		opts = append(opts, http.Address(addr))
	}
	if t := c.GetHttp().GetTimeout(); t != nil {
		opts = append(opts, http.Timeout(t.AsDuration()))
	}

	srv := http.NewServer(opts...)
	for _, register := range registrars {
		register(srv)
	}
	return srv
}

// FileUploadLimitFilter prevents large bodies from reaching protobuf decoding.
func FileUploadLimitFilter(maxBytes int64) http.FilterFunc {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if r.Method == stdhttp.MethodPost && r.URL.Path == "/v1/system/files:upload" {
				r.Body = stdhttp.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
