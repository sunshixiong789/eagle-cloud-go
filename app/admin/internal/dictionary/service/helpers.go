// Package interfaces adapts dictionary use cases to protobuf transports.
package service

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/eagle-go/eagle/app/admin/internal/dictionary/domain"
)

const (
	defaultPageSize int32 = 20
	maxPageSize     int32 = 200
)

// paginate 使用统一的 0-based 页码契约：page=0 是第一页。
// offset 使用 int64，避免合法 int32 参数相乘时先发生 int32 溢出。
func paginate(page, pageSize int32) (offset int64, limit int32) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return int64(page) * int64(pageSize), pageSize
}

func toStatus(v int32) domain.Status   { return domain.Status(v) }
func fromStatus(s domain.Status) int32 { return int32(s) }
func toStatusPtr(v *int32) *domain.Status {
	if v == nil {
		return nil
	}
	s := domain.Status(*v)
	return &s
}

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
