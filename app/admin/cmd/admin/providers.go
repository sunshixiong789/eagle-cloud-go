package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/wire"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	notificationv1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	accessapp "github.com/eagle-go/eagle/app/admin/internal/access/application"
	accessinfra "github.com/eagle-go/eagle/app/admin/internal/access/infrastructure"
	accessservice "github.com/eagle-go/eagle/app/admin/internal/access/service"
	dictionaryinfra "github.com/eagle-go/eagle/app/admin/internal/dictionary/infrastructure"
	dictionaryservice "github.com/eagle-go/eagle/app/admin/internal/dictionary/service"
	fileapp "github.com/eagle-go/eagle/app/admin/internal/file/application"
	filedomain "github.com/eagle-go/eagle/app/admin/internal/file/domain"
	fileinfra "github.com/eagle-go/eagle/app/admin/internal/file/infrastructure"
	fileservice "github.com/eagle-go/eagle/app/admin/internal/file/service"
	notificationapp "github.com/eagle-go/eagle/app/admin/internal/notification/application"
	notificationinfra "github.com/eagle-go/eagle/app/admin/internal/notification/infrastructure"
	notificationservice "github.com/eagle-go/eagle/app/admin/internal/notification/service"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

type policyHealth struct{}
type policyReconciler struct{}
type orderCreatedConsumer struct{}
type inboxJanitor struct{}
type fileCleanupWorker struct{}

var providerSet = wire.NewSet(
	provideData,
	provideAuth,
	provideServer,
	provideFile,
	provideRabbitMQ,
	platformdb.Open,
	accessinfra.NewPolicyStore,
	accessinfra.NewEnforcer,
	accessinfra.NewPermissionRepo,
	accessinfra.NewPolicyRepo,
	accessinfra.NewAuthorizationChecker,
	provideAuthorizer,
	providePolicyHealth,
	providePolicyReconciler,
	accessapp.NewPermissionUsecase,
	accessapp.NewRoleBindingUsecase,
	accessservice.NewPermissionService,
	accessservice.NewRoleBindingService,
	accessservice.NewAuthorizationService,
	dictionaryinfra.NewDictRepo,
	dictionaryservice.NewDictService,
	fileinfra.NewBlobStore,
	fileinfra.NewRepository,
	provideFileUsecase,
	fileservice.NewFileService,
	provideFileCleanupWorker,
	notificationinfra.NewRepository,
	notificationapp.NewUsecase,
	notificationservice.NewNotificationService,
	provideOrderCreatedConsumer,
	provideInboxJanitor,
	server.NewVerifier,
	provideMiddlewares,
	provideGRPCServer,
	provideHTTPServer,
	newComponents,
)

// 默认构建不编译 wire.go，保留对 providerSet 的引用以免被标成未使用。
var _ = providerSet

func provideData(bc *config.Bootstrap) *config.Data     { return bc.GetData() }
func provideAuth(bc *config.Bootstrap) *config.Auth     { return bc.GetAuth() }
func provideServer(bc *config.Bootstrap) *config.Server { return bc.GetServer() }
func provideFile(bc *config.Bootstrap) *config.File     { return bc.GetFile() }
func provideRabbitMQ(bc *config.Bootstrap) *config.Messaging_RabbitMQ {
	return bc.GetMessaging().GetRabbitmq()
}

func provideAuthorizer(enforcer *authz.Enforcer) authz.Authorizer {
	return enforcer
}

func providePolicyHealth(store *accessinfra.PolicyStore, enforcer *authz.Enforcer) (policyHealth, func(), error) {
	return policyHealth{}, accessinfra.RegisterPolicyHealth(store, enforcer), nil
}

func providePolicyReconciler(store *accessinfra.PolicyStore, enforcer *authz.Enforcer, logger *slog.Logger) (policyReconciler, func(), error) {
	return policyReconciler{}, accessinfra.NewPolicyReconciler(store, enforcer, logger), nil
}

func provideFileUsecase(repo filedomain.Repository, blobs filedomain.BlobStore, c *config.File) *fileapp.Usecase {
	return fileapp.NewUsecase(repo, blobs, c.GetMaxSizeBytes())
}

func provideFileCleanupWorker(uc *fileapp.Usecase, logger *slog.Logger) (fileCleanupWorker, func(), error) {
	return fileCleanupWorker{}, fileservice.NewCleanupWorker(uc, logger), nil
}

func provideOrderCreatedConsumer(c *config.Messaging_RabbitMQ, uc *notificationapp.Usecase, logger *slog.Logger) (orderCreatedConsumer, func(), error) {
	return orderCreatedConsumer{}, notificationservice.NewOrderCreatedConsumer(c, uc, logger), nil
}

func provideInboxJanitor(db *platformdb.Database, logger *slog.Logger) (inboxJanitor, func(), error) {
	return inboxJanitor{}, notificationinfra.NewInboxJanitor(db, logger), nil
}

func provideMiddlewares(
	logger *slog.Logger,
	verifier *authn.Verifier,
	authorizer authz.Authorizer,
	auth *config.Auth,
) ([]middleware.Middleware, error) {
	return server.NewMiddlewares(logger, verifier, authorizer, auth, adminErrorMappings()...)
}

func provideGRPCServer(
	c *config.Server,
	ms []middleware.Middleware,
	permission *accessservice.PermissionService,
	role *accessservice.RoleBindingService,
	authorization *accessservice.AuthorizationService,
	dict *dictionaryservice.DictService,
	file *fileservice.FileService,
	notification *notificationservice.NotificationService,
) *grpc.Server {
	return server.NewGRPCServer(c, ms, func(s *grpc.Server) {
		accessv1.RegisterPermissionServiceServer(s, permission)
		accessv1.RegisterRoleBindingServiceServer(s, role)
		accessv1.RegisterAuthorizationServiceServer(s, authorization)
		dictionaryv1.RegisterDictServiceServer(s, dict)
		filev1.RegisterFileServiceServer(s, file)
		notificationv1.RegisterNotificationServiceServer(s, notification)
	})
}

func provideHTTPServer(
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

func newComponents(
	gs *grpc.Server,
	hs *http.Server,
	_ policyHealth,
	_ policyReconciler,
	_ orderCreatedConsumer,
	_ inboxJanitor,
	_ fileCleanupWorker,
) platformruntime.Components {
	return platformruntime.Components{Servers: []transport.Server{gs, hs}}
}
