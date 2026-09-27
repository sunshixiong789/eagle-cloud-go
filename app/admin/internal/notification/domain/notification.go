// Package domain contains the in-app notification model and repository port.
package domain

import (
	"context"
	"errors"
	"time"
)

var ErrNotificationNotFound = errors.New("notification: 通知不存在")

type Notification struct {
	ID               int64
	RecipientSubject string
	SenderSubject    string
	Title            string
	Content          string
	ReadAt           *time.Time
	CreatedAt        time.Time
}

type ListQuery struct {
	RecipientSubject string
	UnreadOnly       bool
	Offset           int64
	PageSize         int32
}

type Repository interface {
	Create(context.Context, *Notification) (*Notification, error)
	List(context.Context, ListQuery) ([]*Notification, int64, error)
	UnreadCount(context.Context, string) (int64, error)
	MarkRead(context.Context, string, int64, time.Time) (*Notification, error)
	MarkAllRead(context.Context, string, time.Time) (int64, error)
	CreateFromEvent(context.Context, string, string, *Notification) (bool, error)
}
