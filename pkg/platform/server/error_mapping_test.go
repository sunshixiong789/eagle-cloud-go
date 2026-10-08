package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware/logging"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	v1 "github.com/eagle-go/eagle/api/eagle/access/v1"
)

func TestToTransportErrorKeepsDistinctReasons(t *testing.T) {
	errInvalidPermissionCode := errors.New("invalid permission code")
	errButtonRequiresCode := errors.New("button requires code")
	errInvalidPermissionType := errors.New("invalid permission type")
	errEmptyPermissionName := errors.New("empty permission name")
	errEmptyRole := errors.New("empty role")
	errUnknownPermissionCode := errors.New("unknown permission code")
	errPermissionNotFound := errors.New("permission not found")
	rules := []ErrorMappingRule{
		BadRequest(errInvalidPermissionCode, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE),
		BadRequest(errButtonRequiresCode, v1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE),
		BadRequest(errInvalidPermissionType, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE),
		BadRequest(errEmptyPermissionName, v1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME),
		BadRequest(errEmptyRole, v1.ErrorReason_ERROR_REASON_EMPTY_ROLE),
		BadRequest(errUnknownPermissionCode, v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE),
		NotFound(errPermissionNotFound, v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND),
	}
	cases := []struct {
		err    error
		reason v1.ErrorReason
		code   int
	}{
		{errInvalidPermissionCode, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE, 400},
		{errButtonRequiresCode, v1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE, 400},
		{errInvalidPermissionType, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE, 400},
		{errEmptyPermissionName, v1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME, 400},
		{errEmptyRole, v1.ErrorReason_ERROR_REASON_EMPTY_ROLE, 400},
		{errUnknownPermissionCode, v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE, 400},
		{errPermissionNotFound, v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND, 404},
	}
	for _, tc := range cases {
		got := toTransportError(fmt.Errorf("adapter detail: %w", tc.err), rules)
		if kerrors.Code(got) != tc.code {
			t.Errorf("%v code = %d, want %d", tc.err, kerrors.Code(got), tc.code)
		}
		if kerrors.Reason(got) != tc.reason.String() {
			t.Errorf("%v reason = %s, want %s", tc.err, kerrors.Reason(got), tc.reason)
		}
		if !errors.Is(got, tc.err) || kerrors.FromError(got).Message != tc.err.Error() {
			t.Errorf("mapped domain error lost its cause or exposed adapter details: %v", got)
		}
	}
}

func TestErrorMappingResponseBoundary(t *testing.T) {
	const internalDetail = "synthetic database failure at private-db:5432"
	cases := []struct {
		name    string
		err     error
		code    int
		grpc    codes.Code
		reason  string
		message string
	}{
		{"unknown", fmt.Errorf("query product: %w", errors.New(internalDetail)), 500, codes.Internal, "INTERNAL_ERROR", "internal server error"},
		{"framework internal", kerrors.InternalServer("DATABASE_FAILURE", internalDetail).WithMetadata(map[string]string{"connection": internalDetail}), 500, codes.Internal, "INTERNAL_ERROR", "internal server error"},
		{"grpc internal", status.Error(codes.Internal, internalDetail), 500, codes.Internal, "INTERNAL_ERROR", "internal server error"},
		{"wrapped cancellation", fmt.Errorf("%s: %w", internalDetail, context.Canceled), 499, codes.Canceled, "CANCELED", "request canceled"},
		{"grpc cancellation", status.Error(codes.Canceled, internalDetail), 499, codes.Canceled, "CANCELED", "request canceled"},
		{"wrapped deadline", fmt.Errorf("%s: %w", internalDetail, context.DeadlineExceeded), 504, codes.DeadlineExceeded, "DEADLINE_EXCEEDED", "request deadline exceeded"},
		{"grpc deadline", status.Error(codes.DeadlineExceeded, internalDetail), 504, codes.DeadlineExceeded, "DEADLINE_EXCEEDED", "request deadline exceeded"},
		{"framework unavailable", kerrors.ServiceUnavailable("UPSTREAM_FAILURE", internalDetail), 503, codes.Unavailable, "SERVICE_UNAVAILABLE", "service unavailable"},
		{"grpc unavailable", status.Error(codes.Unavailable, internalDetail), 503, codes.Unavailable, "SERVICE_UNAVAILABLE", "service unavailable"},
		{"wrapped validation", fmt.Errorf("%s: %w", internalDetail, kerrors.BadRequest("VALIDATOR", "name is required")), 400, codes.InvalidArgument, "VALIDATOR", "name is required"},
		{"conflict", kerrors.Conflict("CONCURRENT_MODIFICATION", "resource changed"), 409, codes.Aborted, "CONCURRENT_MODIFICATION", "resource changed"},
		{"rate limit", kerrors.New(429, "RATELIMIT", "rate limit exceeded"), 429, codes.ResourceExhausted, "RATELIMIT", "rate limit exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := ErrorMapping()(func(context.Context, any) (any, error) { return nil, tc.err })
			_, mapped := handler(context.Background(), nil)
			if errors.Unwrap(mapped) != tc.err {
				t.Fatal("original error was not retained as the cause")
			}
			public := kerrors.FromError(mapped)
			if int(public.Code) != tc.code || public.Reason != tc.reason || public.Message != tc.message {
				t.Fatalf("public error = %v", public)
			}

			response := httptest.NewRecorder()
			kratoshttp.DefaultErrorEncoder(response, httptest.NewRequest("GET", "/", nil), mapped)
			if response.Code != tc.code || strings.Contains(response.Body.String(), internalDetail) {
				t.Fatalf("HTTP response = %d %s", response.Code, response.Body.String())
			}
			grpcStatus := status.Convert(mapped)
			if grpcStatus.Code() != tc.grpc || grpcStatus.Message() != tc.message {
				t.Fatalf("gRPC status = %v", grpcStatus)
			}
			encoded, err := protojson.Marshal(grpcStatus.Proto())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), internalDetail) {
				t.Fatalf("gRPC response exposes internal detail: %s", encoded)
			}
		})
	}
}

func TestErrorMappingRetainsCauseInRequestLog(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	cause := errors.New("synthetic database failure")
	handler := logging.Server(logger)(ErrorMapping()(func(context.Context, any) (any, error) {
		return nil, cause
	}))
	_, mapped := handler(context.Background(), nil)
	if !errors.Is(mapped, cause) {
		t.Fatal("original error is missing from the error chain")
	}
	if !strings.Contains(output.String(), cause.Error()) || !strings.Contains(output.String(), "INTERNAL_ERROR") {
		t.Fatalf("request log is missing the public reason or internal cause: %s", output.String())
	}
}

func TestToTransportErrorKeepsSuccess(t *testing.T) {
	if err := toTransportError(nil, nil); err != nil {
		t.Fatalf("successful response returned an error: %v", err)
	}
}
