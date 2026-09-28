package infrastructure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type S3BlobStore struct {
	client *minio.Client
	bucket string
}

func NewS3BlobStore(config *config.File_S3) (*S3BlobStore, error) {
	client, err := minio.New(config.GetEndpoint(), &minio.Options{
		Creds:  credentials.NewStaticV4(config.GetAccessKey(), config.GetSecretKey(), ""),
		Secure: config.GetUseSsl(), Region: config.GetRegion(),
	})
	if err != nil {
		return nil, fmt.Errorf("file: create S3 client: %w", err)
	}
	// Object storage availability must not prevent identity and policy services
	// from starting. Requests report storage errors and recover without restart.
	return &S3BlobStore{client: client, bucket: config.GetBucket()}, nil
}

func (s *S3BlobStore) Put(ctx context.Context, key string, content []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("file: put S3 object: %w", err)
	}
	return nil
}

func (s *S3BlobStore) Read(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, s.translate("get", err)
	}
	defer func() { _ = object.Close() }()
	content, err := io.ReadAll(object)
	if err != nil {
		return nil, s.translate("read", err)
	}
	return content, nil
}

func (s *S3BlobStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		translated := s.translate("delete", err)
		if errors.Is(translated, domain.ErrFileNotFound) {
			return nil
		}
		return translated
	}
	return nil
}

func (s *S3BlobStore) translate(operation string, err error) error {
	response := minio.ToErrorResponse(err)
	if response.Code == "NoSuchKey" || response.Code == "NoSuchObject" || response.StatusCode == 404 {
		return domain.ErrFileNotFound
	}
	return fmt.Errorf("file: %s S3 object: %w", operation, err)
}
