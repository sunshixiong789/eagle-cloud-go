package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/file/application"
)

const (
	cleanupInterval = 10 * time.Minute
	staleFileAge    = time.Hour
	cleanupBatch    = 100
)

// NewCleanupWorker 清理上传中断和删除失败留下的对象与元数据。
func NewCleanupWorker(uc *application.Usecase, logger *slog.Logger) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleaned, err := uc.CleanupStale(ctx, time.Now().Add(-staleFileAge), cleanupBatch)
				if err != nil {
					logger.WarnContext(ctx, "cleanup stale files failed", "error", err)
				} else if cleaned > 0 {
					logger.InfoContext(ctx, "cleaned stale files", "count", cleaned)
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}
