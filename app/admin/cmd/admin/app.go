package main

import (
	"log/slog"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	notificationv1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	accessdomain "github.com/eagle-go/eagle/app/admin/internal/access/domain"
	dictionarydomain "github.com/eagle-go/eagle/app/admin/internal/dictionary/domain"
	filedomain "github.com/eagle-go/eagle/app/admin/internal/file/domain"
	notificationdomain "github.com/eagle-go/eagle/app/admin/internal/notification/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

//go:generate go run github.com/google/wire/cmd/wire

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	components, cleanup, err := wireApp(bc, logger)
	if err != nil {
		return platformruntime.Components{}, err
	}
	components.Cleanup = cleanup
	return components, nil
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
