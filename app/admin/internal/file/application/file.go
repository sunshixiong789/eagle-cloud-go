// Package application coordinates file metadata and blob storage.
package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
)

type Usecase struct {
	repo    domain.Repository
	blobs   domain.BlobStore
	maxSize int64
}

func NewUsecase(repo domain.Repository, blobs domain.BlobStore, maxSize int64) *Usecase {
	return &Usecase{repo: repo, blobs: blobs, maxSize: maxSize}
}

func (uc *Usecase) Upload(ctx context.Context, owner, name, contentType string, content []byte) (*domain.File, error) {
	if int64(len(content)) > uc.maxSize {
		return nil, domain.ErrFileTooLarge
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(content)
	file, err := domain.NewFile(id, owner, name, contentType, int64(len(content)), hex.EncodeToString(hash[:]))
	if err != nil {
		return nil, err
	}
	pending, err := uc.repo.CreatePending(ctx, file)
	if err != nil {
		return nil, err
	}
	if err := uc.blobs.Put(ctx, pending.StorageKey(), content); err != nil {
		cleanupErr := uc.repo.DeleteMetadata(ctx, pending.ID())
		return nil, errors.Join(err, cleanupErr)
	}
	ready, err := uc.repo.MarkReady(ctx, pending.ID())
	if err != nil {
		// 保留 pending 记录，后台清理任务会删除对象和元数据。
		return nil, err
	}
	return ready, nil
}

func (uc *Usecase) Get(ctx context.Context, owner, id string) (*domain.File, error) {
	return uc.repo.GetReadyOwned(ctx, owner, id)
}

func (uc *Usecase) Download(ctx context.Context, owner, id string) (*domain.File, []byte, error) {
	file, err := uc.repo.GetReadyOwned(ctx, owner, id)
	if err != nil {
		return nil, nil, err
	}
	content, err := uc.blobs.Read(ctx, file.StorageKey())
	if err != nil {
		return nil, nil, err
	}
	return file, content, nil
}

func (uc *Usecase) List(ctx context.Context, q domain.ListQuery) ([]*domain.File, int64, error) {
	return uc.repo.ListReady(ctx, q)
}

func (uc *Usecase) Delete(ctx context.Context, owner, id string) error {
	file, err := uc.repo.MarkDeleting(ctx, owner, id)
	if err != nil {
		return err
	}
	if err := uc.blobs.Delete(ctx, file.StorageKey()); err != nil {
		return err
	}
	return uc.repo.DeleteMetadata(ctx, file.ID())
}

// CleanupStale 收敛上传中断和删除失败留下的非 READY 记录。
func (uc *Usecase) CleanupStale(ctx context.Context, before time.Time, limit int) (int, error) {
	files, err := uc.repo.ListStale(ctx, before, limit)
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, file := range files {
		if err := uc.blobs.Delete(ctx, file.StorageKey()); err != nil {
			return cleaned, err
		}
		if err := uc.repo.DeleteMetadata(ctx, file.ID()); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate file id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
