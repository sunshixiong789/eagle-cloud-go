package infrastructure

import (
	"fmt"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

func NewBlobStore(c *config.File) (domain.BlobStore, error) {
	switch c.GetProvider() {
	case "local":
		return NewLocalBlobStore(c.GetLocalDir())
	case "s3":
		return NewS3BlobStore(c.GetS3())
	default:
		return nil, fmt.Errorf("file: unsupported blob provider %q", c.GetProvider())
	}
}
