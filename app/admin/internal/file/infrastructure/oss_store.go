package infrastructure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type OSSBlobStore struct {
	client *oss.Client
	bucket string
}

func NewOSSBlobStore(c *config.File_OSS) *OSSBlobStore {
	cfg := oss.LoadDefaultConfig().WithRegion(c.GetRegion()).WithCredentialsProvider(
		credentials.NewStaticCredentialsProvider(c.GetAccessKeyId(), c.GetAccessKeySecret(), c.GetSecurityToken()),
	)
	if c.GetEndpoint() != "" {
		cfg.WithEndpoint(c.GetEndpoint())
	}
	// Construction performs no network I/O; an OSS outage must not prevent
	// identity and policy services from starting.
	return &OSSBlobStore{client: oss.NewClient(cfg), bucket: c.GetBucket()}
}

func (s *OSSBlobStore) Put(ctx context.Context, key string, content []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.client.PutObject(ctx, &oss.PutObjectRequest{
		Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), Body: bytes.NewReader(content),
		ContentType: oss.Ptr("application/octet-stream"),
	})
	if err != nil {
		return fmt.Errorf("file: put OSS object: %w", err)
	}
	return nil
}

func (s *OSSBlobStore) Read(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	object, err := s.client.GetObject(ctx, &oss.GetObjectRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key)})
	if err != nil {
		return nil, translateOSSError("get", err)
	}
	defer func() { _ = object.Body.Close() }()
	content, err := io.ReadAll(object.Body)
	if err != nil {
		return nil, translateOSSError("read", err)
	}
	return content, nil
}

func (s *OSSBlobStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.client.DeleteObject(ctx, &oss.DeleteObjectRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key)})
	if err != nil {
		translated := translateOSSError("delete", err)
		if errors.Is(translated, domain.ErrFileNotFound) {
			return nil
		}
		return translated
	}
	return nil
}

func translateOSSError(operation string, err error) error {
	var serviceError *oss.ServiceError
	if errors.As(err, &serviceError) && serviceError.Code == "NoSuchKey" {
		return domain.ErrFileNotFound
	}
	return fmt.Errorf("file: %s OSS object: %w", operation, err)
}
