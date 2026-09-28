package main

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v3/middleware"
	"google.golang.org/genproto/googleapis/api/httpbody"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"

	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	accessservice "github.com/eagle-go/eagle/app/admin/internal/access/service"
	dictionaryservice "github.com/eagle-go/eagle/app/admin/internal/dictionary/service"
	fileapp "github.com/eagle-go/eagle/app/admin/internal/file/application"
	filedomain "github.com/eagle-go/eagle/app/admin/internal/file/domain"
	fileinfra "github.com/eagle-go/eagle/app/admin/internal/file/infrastructure"
	fileservice "github.com/eagle-go/eagle/app/admin/internal/file/service"
	notificationservice "github.com/eagle-go/eagle/app/admin/internal/notification/service"
	"github.com/eagle-go/eagle/pkg/identity"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

type fileMemoryRepository struct {
	filedomain.Repository
	file *filedomain.File
}

func (r *fileMemoryRepository) CreatePending(_ context.Context, f *filedomain.File) (*filedomain.File, error) {
	r.file = f
	return f, nil
}
func (r *fileMemoryRepository) MarkReady(context.Context, string) (*filedomain.File, error) {
	return r.file, nil
}
func (r *fileMemoryRepository) GetReadyOwned(context.Context, string, string) (*filedomain.File, error) {
	return r.file, nil
}

func TestFileGRPCUploadDownloadLimits(t *testing.T) {
	const limit = 10 << 20
	blobs, err := fileinfra.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := fileservice.NewFileService(fileapp.NewUsecase(&fileMemoryRepository{}, blobs, limit))
	// This transport-size regression isolates authentication; signed-token paths
	// are exercised by the end-to-end tests.
	principal := func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			return next(identity.NewContext(ctx, &identity.Principal{Subject: "owner"}), req)
		}
	}
	ms := []middleware.Middleware{principal, server.ErrorMapping(adminErrorMappings()...)}
	srv := newGRPCServer(&config.Server{Grpc: &config.Server_GRPC{Timeout: durationpb.New(10 * time.Second)}}, ms, &config.File{MaxSizeBytes: limit}, accessservice.NewPermissionService(nil), accessservice.NewRoleBindingService(nil), accessservice.NewAuthorizationService(nil), dictionaryservice.NewDictService(nil), svc, notificationservice.NewNotificationService(nil))
	listener := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(listener) }()
	defer srv.Server.Stop()
	conn, err := grpc.NewClient("passthrough:///file-size-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(limit+config.FileMessageOverhead)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	client := filev1.NewFileServiceClient(conn)
	for _, size := range []int{5 << 20, limit} {
		payload := bytes.Repeat([]byte("a"), size)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		uploaded, err := client.UploadFile(ctx, &filev1.UploadFileRequest{Filename: "file.bin", Content: &httpbody.HttpBody{ContentType: "application/octet-stream", Data: payload}})
		if err != nil {
			cancel()
			t.Fatalf("upload %d: %v", size, err)
		}
		downloaded, err := client.DownloadFile(ctx, &filev1.DownloadFileRequest{Id: uploaded.GetFile().GetId()})
		cancel()
		if err != nil || !bytes.Equal(downloaded.GetContent().GetData(), payload) {
			t.Fatalf("download %d: %v", size, err)
		}
	}
	_, err = client.UploadFile(context.Background(), &filev1.UploadFileRequest{Filename: "too-large.bin", Content: &httpbody.HttpBody{Data: make([]byte, limit+1)}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversize business error=%v", err)
	}
}
