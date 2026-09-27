package infrastructure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

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
	store := &S3BlobStore{client: client, bucket: config.GetBucket()}
	exists, err := client.BucketExists(context.Background(), store.bucket)
	if err != nil {
		return nil, fmt.Errorf("file: check S3 bucket: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("file: S3 bucket %q does not exist", store.bucket)
	}
	return store, nil
}

func (s *S3BlobStore) Put(ctx context.Context, key string, content []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("file: put S3 object: %w", err)
	}
	return nil
}

func (s *S3BlobStore) Read(ctx context.Context, key string) ([]byte, error) {
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
