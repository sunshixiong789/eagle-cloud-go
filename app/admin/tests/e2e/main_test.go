// Package e2e 端到端验证真实服务：真实 PostgreSQL、真实迁移、
// 真实 HTTP 服务器、真实签名的 JWT，走完整的中间件链。
//
// 与各模块 infrastructure 集成测试的区别：那里验证仓储实现与 SQL，
// 这里验证的是「服务作为一个整体能不能起来并正确响应」——
// 配置解析、依赖装配、中间件顺序、认证与授权判定、错误码映射。
//
// 不依赖 Docker：PostgreSQL 由 embedded-postgres 在进程内拉起，
// Keycloak 用一个签发真实 RS256 token 的替身。
package e2e

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/protobuf/types/known/durationpb"

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
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/identity"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/platform/server"
	"github.com/eagle-go/eagle/tests/testkit"
)

const (
	clientID  = "eagle-system"
	adminRole = "admin"
)

var testDSN string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	pg, err := testkit.StartPostgres("e2e", "eagle_e2e")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e 环境准备失败: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = pg.Close() }()
	testDSN = pg.DSN
	os.Exit(m.Run())
}

// testEnv 是一次测试用的完整服务实例。
type testEnv struct {
	http     *httptest.Server
	kc       *fakeKeycloak
	enforcer *authz.Enforcer
	policy   accessdomain.PolicyRepo
}

// newTestEnv 用与生产完全相同的构造函数装配服务。
//
// 刻意不走 buildApp：那个函数返回 *kratos.App，会真正监听端口。
// 这里逐个调用同样的 provider，把 http.Server 交给 httptest——
// 装配路径与生产一致，但不占用固定端口，测试可以并行。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("需要真实数据库")
	}

	kc := newFakeKeycloak(t)

	authConf := &config.Auth{
		Issuer:         kc.issuer(),
		ClientId:       clientID,
		Audience:       clientID,
		SuperAdminRole: adminRole,
	}

	adminDB, cleanup, err := platformdb.Open(&config.Data{Database: &config.Data_Database{
		Dsn: testDSN, MaxConns: 4, MaxIdleConns: 1,
	}})
	if err != nil {
		t.Fatalf("构造 admin Data: %v", err)
	}
	t.Cleanup(cleanup)

	store := accessinfra.NewPolicyStore(adminDB)

	enforcer, err := accessinfra.NewEnforcer(store)
	if err != nil {
		t.Fatalf("构造 Casbin enforcer: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	verifier := server.NewVerifier(authConf)
	middlewares, err := server.NewMiddlewares(logger, verifier, enforcer, authConf, e2eErrorMappings()...)
	if err != nil {
		t.Fatalf("构造中间件链: %v", err)
	}

	t.Cleanup(accessinfra.NewPolicyReconciler(store, enforcer, logger))

	permRepo := accessinfra.NewPermissionRepo(adminDB)
	policyRepo := accessinfra.NewPolicyRepo(enforcer, store)
	dictRepo := dictionaryinfra.NewDictRepo(adminDB)
	fileRepo := fileinfra.NewRepository(adminDB)
	notificationRepo := notificationinfra.NewRepository(adminDB)
	blobs, err := fileinfra.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("构造本地文件存储: %v", err)
	}

	permSvc := accessservice.NewPermissionService(accessapp.NewPermissionUsecase(permRepo, policyRepo))
	dictSvc := dictionaryservice.NewDictService(dictRepo)
	bindingSvc := accessservice.NewRoleBindingService(accessapp.NewRoleBindingUsecase(policyRepo, permRepo))
	fileSvc := fileservice.NewFileService(fileapp.NewUsecase(fileRepo, blobs, 10<<20))
	notificationSvc := notificationservice.NewNotificationService(notificationapp.NewUsecase(notificationRepo))

	// addr 留空：不监听真实端口，只把 Server 当 http.Handler 用
	srv := server.NewHTTPServer(&config.Server{
		Http: &config.Server_HTTP{Timeout: durationpb.New(10 * time.Second)},
	}, middlewares, []kratoshttp.FilterFunc{server.FileUploadLimitFilter(10 << 20)}, func(s *kratoshttp.Server) {
		accessv1.RegisterPermissionServiceHTTPServer(s, permSvc)
		accessv1.RegisterRoleBindingServiceHTTPServer(s, bindingSvc)
		dictionaryv1.RegisterDictServiceHTTPServer(s, dictSvc)
		filev1.RegisterFileServiceHTTPServer(s, fileSvc)
		notificationv1.RegisterNotificationServiceHTTPServer(s, notificationSvc)
	})

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return &testEnv{http: ts, kc: kc, enforcer: enforcer, policy: policyRepo}
}

func e2eErrorMappings() []server.ErrorMappingRule {
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

// grantRole 给角色授予权限码，并让判定器立即生效。
func (e *testEnv) grantRole(t *testing.T, role string, perms ...string) {
	t.Helper()
	if !identity.ValidRoleKey(role) {
		role = identity.RealmRoleKey(role)
	}
	roleValue, err := accessdomain.NewRole(role)
	if err != nil {
		t.Fatalf("构造角色 %s: %v", role, err)
	}
	codes, err := accessdomain.ParsePermissionCodes(perms)
	if err != nil {
		t.Fatalf("解析权限码: %v", err)
	}
	binding, err := accessdomain.NewRoleBinding(roleValue, codes)
	if err != nil {
		t.Fatalf("构造角色绑定: %v", err)
	}
	if _, err := e.policy.SaveBinding(context.Background(), binding, nil); err != nil {
		t.Fatalf("授予角色 %s 权限: %v", role, err)
	}
}
