// Package interfaces adapts file use cases to protobuf transports.
package service

import (
	"context"
	"mime"

	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/genproto/googleapis/api/httpbody"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	"github.com/eagle-go/eagle/app/admin/internal/file/application"
	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
	"github.com/eagle-go/eagle/pkg/identity"
)

type FileService struct {
	v1.UnimplementedFileServiceServer
	uc *application.Usecase
}

func NewFileService(uc *application.Usecase) *FileService { return &FileService{uc: uc} }

func toProto(file *domain.File) *v1.File {
	if file == nil {
		return nil
	}
	return &v1.File{
		Id: file.ID(), Name: file.Name(), ContentType: file.ContentType(),
		Size: file.Size(), Sha256: file.SHA256(), CreatedAt: timestamppb.New(file.CreatedAt()),
	}
}

func (s *FileService) UploadFile(ctx context.Context, req *v1.UploadFileRequest) (*v1.UploadFileResponse, error) {
	body := req.GetContent()
	file, err := s.uc.Upload(ctx, identity.Subject(ctx), req.GetFilename(), body.GetContentType(), body.GetData())
	if err != nil {
		return nil, err
	}
	return &v1.UploadFileResponse{File: toProto(file)}, nil
}

func (s *FileService) GetFile(ctx context.Context, req *v1.GetFileRequest) (*v1.GetFileResponse, error) {
	file, err := s.uc.Get(ctx, identity.Subject(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &v1.GetFileResponse{File: toProto(file)}, nil
}

func (s *FileService) DownloadFile(ctx context.Context, req *v1.DownloadFileRequest) (*v1.DownloadFileResponse, error) {
	file, content, err := s.uc.Download(ctx, identity.Subject(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	if tr, ok := transport.FromServerContext(ctx); ok && tr.Kind() == transport.KindHTTP {
		tr.ReplyHeader().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name()}))
	}
	return &v1.DownloadFileResponse{Content: &httpbody.HttpBody{
		ContentType: file.ContentType(),
		Data:        content,
	}}, nil
}

func (s *FileService) ListMyFiles(ctx context.Context, req *v1.ListMyFilesRequest) (*v1.ListMyFilesResponse, error) {
	offset, limit := paginate(req.GetPage(), req.GetPageSize())
	files, total, err := s.uc.List(ctx, domain.ListQuery{OwnerSubject: identity.Subject(ctx), Offset: offset, PageSize: limit})
	if err != nil {
		return nil, err
	}
	out := make([]*v1.File, 0, len(files))
	for _, file := range files {
		out = append(out, toProto(file))
	}
	return &v1.ListMyFilesResponse{Files: out, Total: total}, nil
}

func (s *FileService) DeleteFile(ctx context.Context, req *v1.DeleteFileRequest) (*v1.DeleteFileResponse, error) {
	if err := s.uc.Delete(ctx, identity.Subject(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &v1.DeleteFileResponse{}, nil
}

const defaultPageSize int32 = 20

func paginate(page, size int32) (int64, int32) {
	if size <= 0 {
		size = defaultPageSize
	}
	if size > 200 {
		size = 200
	}
	return int64(page) * int64(size), size
}
