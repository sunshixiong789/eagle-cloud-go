package main

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"
	stdgrpc "google.golang.org/grpc"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	notificationv1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	accessservice "github.com/eagle-go/eagle/app/admin/internal/access/service"
	dictionaryservice "github.com/eagle-go/eagle/app/admin/internal/dictionary/service"
	fileservice "github.com/eagle-go/eagle/app/admin/internal/file/service"
	notificationservice "github.com/eagle-go/eagle/app/admin/internal/notification/service"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func newGRPCServer(
	c *config.Server,
	ms []middleware.Middleware,
	fileConf *config.File,
	permission *accessservice.PermissionService,
	role *accessservice.RoleBindingService,
	authorization *accessservice.AuthorizationService,
	dict *dictionaryservice.DictService,
	file *fileservice.FileService,
	notification *notificationservice.NotificationService,
) *grpc.Server {
	// File metadata and protobuf framing need space beyond the raw file limit.
	options := []grpc.ServerOption{grpc.Options(stdgrpc.MaxRecvMsgSize(int(fileConf.GetMaxSizeBytes()) + config.FileMessageOverhead))}
	return server.NewGRPCServer(c, ms, options, func(s *grpc.Server) {
		accessv1.RegisterPermissionServiceServer(s, permission)
		accessv1.RegisterRoleBindingServiceServer(s, role)
		accessv1.RegisterAuthorizationServiceServer(s, authorization)
		dictionaryv1.RegisterDictServiceServer(s, dict)
		filev1.RegisterFileServiceServer(s, file)
		notificationv1.RegisterNotificationServiceServer(s, notification)
	})
}

func newHTTPServer(
	c *config.Server,
	ms []middleware.Middleware,
	fileConf *config.File,
	permission *accessservice.PermissionService,
	role *accessservice.RoleBindingService,
	dict *dictionaryservice.DictService,
	file *fileservice.FileService,
	notification *notificationservice.NotificationService,
) *http.Server {
	return server.NewHTTPServer(c, ms, []http.FilterFunc{
		server.FileUploadLimitFilter(fileConf.GetMaxSizeBytes()),
	}, func(s *http.Server) {
		accessv1.RegisterPermissionServiceHTTPServer(s, permission)
		accessv1.RegisterRoleBindingServiceHTTPServer(s, role)
		dictionaryv1.RegisterDictServiceHTTPServer(s, dict)
		filev1.RegisterFileServiceHTTPServer(s, file)
		notificationv1.RegisterNotificationServiceHTTPServer(s, notification)
	})
}
