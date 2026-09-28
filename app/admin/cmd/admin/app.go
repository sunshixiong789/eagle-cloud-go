package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/transport"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	notificationv1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	accessapp "github.com/eagle-go/eagle/app/admin/internal/access/application"
	accessdomain "github.com/eagle-go/eagle/app/admin/internal/access/domain"
	accessinfra "github.com/eagle-go/eagle/app/admin/internal/access/infrastructure"
	accessservice "github.com/eagle-go/eagle/app/admin/internal/access/service"
	dictionarydomain "github.com/eagle-go/eagle/app/admin/internal/dictionary/domain"
	dictionaryinfra "github.com/eagle-go/eagle/app/admin/internal/dictionary/infrastructure"
	dictionaryservice "github.com/eagle-go/eagle/app/admin/internal/dictionary/service"
	fileapp "github.com/eagle-go/eagle/app/admin/internal/file/application"
	filedomain "github.com/eagle-go/eagle/app/admin/internal/file/domain"
	fileinfra "github.com/eagle-go/eagle/app/admin/internal/file/infrastructure"
	fileservice "github.com/eagle-go/eagle/app/admin/internal/file/service"
	notificationapp "github.com/eagle-go/eagle/app/admin/internal/notification/application"
	notificationdomain "github.com/eagle-go/eagle/app/admin/internal/notification/domain"
	notificationinfra "github.com/eagle-go/eagle/app/admin/internal/notification/infrastructure"
	notificationservice "github.com/eagle-go/eagle/app/admin/internal/notification/service"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	db, closeDB, err := platformdb.Open(bc.GetData())
	if err != nil {
		return platformruntime.Components{}, err
	}
	policyStore := accessinfra.NewPolicyStore(db)
	enforcer, err := accessinfra.NewEnforcer(policyStore)
	if err != nil {
		closeDB()
		return platformruntime.Components{}, err
	}
	ms, err := server.NewMiddlewares(logger, server.NewVerifier(bc.GetAuth()), enforcer, bc.GetAuth(), adminErrorMappings()...)
	if err != nil {
		closeDB()
		return platformruntime.Components{}, err
	}
	blobs, err := fileinfra.NewBlobStore(bc.GetFile())
	if err != nil {
		closeDB()
		return platformruntime.Components{}, err
	}

	permissions := accessinfra.NewPermissionRepo(db)
	policies := accessinfra.NewPolicyRepo(enforcer, policyStore)
	permission := accessservice.NewPermissionService(accessapp.NewPermissionUsecase(permissions, policies))
	role := accessservice.NewRoleBindingService(accessapp.NewRoleBindingUsecase(policies, permissions))
	authorization := accessservice.NewAuthorizationService(accessinfra.NewAuthorizationChecker(enforcer, policyStore))
	dict := dictionaryservice.NewDictService(dictionaryinfra.NewDictRepo(db))
	files := fileapp.NewUsecase(fileinfra.NewRepository(db), blobs, bc.GetFile().GetMaxSizeBytes())
	file := fileservice.NewFileService(files)
	notifications := notificationapp.NewUsecase(notificationinfra.NewRepository(db))
	notification := notificationservice.NewNotificationService(notifications)
	gs := newGRPCServer(bc.GetServer(), ms, bc.GetFile(), permission, role, authorization, dict, file, notification)
	hs := newHTTPServer(bc.GetServer(), ms, bc.GetFile(), permission, role, dict, file, notification)

	// 所有可能失败的初始化完成后，再启动后台任务。
	unregisterPolicyHealth := accessinfra.RegisterPolicyHealth(policyStore, enforcer)
	stopPolicyReconciler := accessinfra.NewPolicyReconciler(policyStore, enforcer, logger)
	stopConsumer := notificationservice.NewOrderCreatedConsumer(bc.GetMessaging().GetRabbitmq(), notifications, logger)
	stopInboxJanitor := notificationinfra.NewInboxJanitor(db, logger)
	stopFileCleanup := fileservice.NewCleanupWorker(files, logger)
	return platformruntime.Components{
		Servers: []transport.Server{gs, hs},
		Cleanup: func() {
			stopFileCleanup()
			stopInboxJanitor()
			stopConsumer()
			stopPolicyReconciler()
			unregisterPolicyHealth()
			closeDB()
		},
	}, nil
}

func adminErrorMappings() []server.ErrorMappingRule {
	return []server.ErrorMappingRule{
		server.NotFound(accessdomain.ErrPermissionNotFound, accessv1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND),
		server.Conflict(accessdomain.ErrPermissionCodeDuplicated, accessv1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED),
		server.Conflict(accessdomain.ErrPermissionHasChildren, accessv1.ErrorReason_ERROR_REASON_PERMISSION_HAS_CHILDREN),
		server.BadRequest(accessdomain.ErrPermissionCycle, accessv1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE),
		server.Conflict(accessdomain.ErrConcurrentModification, accessv1.ErrorReason_ERROR_REASON_CONCURRENT_MODIFICATION),
		server.BadRequest(accessdomain.ErrInvalidPermissionCode, accessv1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE),
		server.BadRequest(accessdomain.ErrButtonRequiresCode, accessv1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE),
		server.BadRequest(accessdomain.ErrInvalidPermissionType, accessv1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE),
		server.BadRequest(accessdomain.ErrEmptyPermissionName, accessv1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME),
		server.NotFound(accessdomain.ErrRoleNotBound, accessv1.ErrorReason_ERROR_REASON_ROLE_NOT_BOUND),
		server.BadRequest(accessdomain.ErrUnknownPermissionCode, accessv1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE),
		server.BadRequest(accessdomain.ErrEmptyRole, accessv1.ErrorReason_ERROR_REASON_EMPTY_ROLE),
		server.BadRequest(accessdomain.ErrSelfInheritance, accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE),
		server.BadRequest(accessdomain.ErrRoleInheritanceCycle, accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE),
		server.NotFound(dictionarydomain.ErrDictTypeNotFound, dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND),
		server.Conflict(dictionarydomain.ErrDictTypeDuplicated, dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED),
		server.NotFound(dictionarydomain.ErrDictDataNotFound, dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND),
		server.Conflict(dictionarydomain.ErrDictDataDuplicated, dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED),
		server.NotFound(filedomain.ErrFileNotFound, filev1.ErrorReason_ERROR_REASON_FILE_NOT_FOUND),
		server.BadRequest(filedomain.ErrFileTooLarge, filev1.ErrorReason_ERROR_REASON_FILE_TOO_LARGE),
		server.BadRequest(filedomain.ErrInvalidFileName, filev1.ErrorReason_ERROR_REASON_INVALID_FILE_NAME),
		server.BadRequest(filedomain.ErrInvalidContentType, filev1.ErrorReason_ERROR_REASON_INVALID_FILE_CONTENT_TYPE),
		server.NotFound(notificationdomain.ErrNotificationNotFound, notificationv1.ErrorReason_ERROR_REASON_NOTIFICATION_NOT_FOUND),
	}
}
