package server

import (
	"context"
	"errors"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	httpstatus "github.com/go-kratos/kratos/v3/transport/http/status"
)

type errorReason interface{ String() string }

// ErrorMappingRule keeps transport error ownership in each service composition
// root while the conversion mechanism remains shared.
type ErrorMappingRule struct {
	domainErr error
	toKratos  func(error) *kerrors.Error
}

func NotFound(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.NotFound(reason.String(), domainErr.Error())
	}}
}

func Conflict(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.Conflict(reason.String(), domainErr.Error())
	}}
}

func BadRequest(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.BadRequest(reason.String(), domainErr.Error())
	}}
}

func ErrorMapping(rules ...ErrorMappingRule) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			resp, err := handler(ctx, req)
			return resp, toTransportError(err, rules)
		}
	}
}

func toTransportError(err error, rules []ErrorMappingRule) error {
	if err == nil {
		return nil
	}
	for _, rule := range rules {
		if errors.Is(err, rule.domainErr) {
			return rule.toKratos(err).WithCause(err)
		}
	}
	public := kerrors.FromError(err)
	switch {
	case errors.Is(err, context.Canceled) || public.Code == httpstatus.ClientClosed:
		public = kerrors.New(httpstatus.ClientClosed, "CANCELED", "request canceled")
	case errors.Is(err, context.DeadlineExceeded) || public.Code == http.StatusGatewayTimeout:
		public = kerrors.GatewayTimeout("DEADLINE_EXCEEDED", "request deadline exceeded")
	case public.Code == http.StatusServiceUnavailable:
		public = kerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "service unavailable")
	case public.Code >= http.StatusInternalServerError:
		public = kerrors.New(int(public.Code), "INTERNAL_ERROR", "internal server error")
	}
	// The outer request logger can still inspect the cause; protocol encoders
	// serialize only the public status, never technical messages or metadata.
	return public.WithCause(err)
}
