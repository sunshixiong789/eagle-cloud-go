// Package client builds the common middleware chain for synchronous upstream calls.
package client

import (
	"fmt"

	kratosmetrics "github.com/go-kratos/kratos/contrib/otel/v3/metrics"
	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/middleware/circuitbreaker"
	"github.com/go-kratos/kratos/v3/middleware/metadata"
	"go.opentelemetry.io/otel"
)

const meterName = "github.com/eagle-go/eagle/pkg/platform/client"

func NewMiddlewares(authentication middleware.Middleware) ([]middleware.Middleware, error) {
	meter := otel.Meter(meterName)
	requests, err := kratosmetrics.DefaultRequestsCounter(meter, kratosmetrics.DefaultClientRequestsCounterName)
	if err != nil {
		return nil, fmt.Errorf("client: build request counter: %w", err)
	}
	seconds, err := kratosmetrics.DefaultSecondsHistogram(meter, kratosmetrics.DefaultClientSecondsHistogramName)
	if err != nil {
		return nil, fmt.Errorf("client: build latency histogram: %w", err)
	}
	middlewares := []middleware.Middleware{
		tracing.Client(),
		kratosmetrics.Client(kratosmetrics.WithRequests(requests), kratosmetrics.WithSeconds(seconds)),
		metadata.Client(metadata.WithPropagatedPrefix("x-md-global-")),
		circuitbreaker.Client(),
	}
	if authentication != nil {
		middlewares = append(middlewares, authentication)
	}
	return middlewares, nil
}
