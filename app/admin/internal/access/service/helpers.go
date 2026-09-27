// Package service adapts the access module to protobuf transports.
package service

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
)

// ── 领域值对象 ↔ proto 标量 ────────────────────────────────
//
// proto 用 int32 表达状态，领域层用 Status 值对象。转换收敛在这里，
// 避免类型转换散落进每个 handler。

func fromStatus(s domain.Status) int32 { return int32(s) }

// toStatusPtr 转换可选的状态过滤条件，nil 表示不按状态过滤。
func toStatusPtr(v *int32) *domain.Status {
	if v == nil {
		return nil
	}
	s := domain.Status(*v)
	return &s
}

// toPermissionTypePtr 转换可选的节点类型过滤条件。
func toPermissionTypePtr(v *int32) *domain.PermissionType {
	if v == nil {
		return nil
	}
	t := domain.PermissionType(*v)
	return &t
}

// ts 把时间转成 protobuf 时间戳，零值时间返回 nil 而不是 1970-01-01。
func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
