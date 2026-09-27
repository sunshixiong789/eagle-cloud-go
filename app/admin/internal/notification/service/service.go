// Package interfaces adapts notification use cases to protobuf transports.
package service

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	"github.com/eagle-go/eagle/app/admin/internal/notification/application"
	"github.com/eagle-go/eagle/app/admin/internal/notification/domain"
	"github.com/eagle-go/eagle/pkg/identity"
)

type NotificationService struct {
	v1.UnimplementedNotificationServiceServer
	uc *application.Usecase
}

func NewNotificationService(uc *application.Usecase) *NotificationService {
	return &NotificationService{uc: uc}
}

func toProto(value *domain.Notification) *v1.Notification {
	if value == nil {
		return nil
	}
	var readAt *timestamppb.Timestamp
	if value.ReadAt != nil {
		readAt = timestamppb.New(*value.ReadAt)
	}
	return &v1.Notification{
		Id: value.ID, Title: value.Title, Content: value.Content,
		SenderSubject: value.SenderSubject, ReadAt: readAt,
		CreatedAt: timestamppb.New(value.CreatedAt),
	}
}

func (s *NotificationService) SendNotification(ctx context.Context, req *v1.SendNotificationRequest) (*v1.SendNotificationResponse, error) {
	value, err := s.uc.Send(ctx, identity.Subject(ctx), req.GetRecipientSubject(), req.GetTitle(), req.GetContent())
	if err != nil {
		return nil, err
	}
	return &v1.SendNotificationResponse{Notification: toProto(value)}, nil
}

func (s *NotificationService) ListMyNotifications(ctx context.Context, req *v1.ListMyNotificationsRequest) (*v1.ListMyNotificationsResponse, error) {
	offset, limit := paginate(req.GetPage(), req.GetPageSize())
	values, total, err := s.uc.List(ctx, domain.ListQuery{
		RecipientSubject: identity.Subject(ctx), UnreadOnly: req.GetUnreadOnly(),
		Offset: offset, PageSize: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*v1.Notification, 0, len(values))
	for _, value := range values {
		out = append(out, toProto(value))
	}
	return &v1.ListMyNotificationsResponse{Notifications: out, Total: total}, nil
}

func (s *NotificationService) GetMyUnreadCount(ctx context.Context, _ *v1.GetMyUnreadCountRequest) (*v1.GetMyUnreadCountResponse, error) {
	count, err := s.uc.UnreadCount(ctx, identity.Subject(ctx))
	if err != nil {
		return nil, err
	}
	return &v1.GetMyUnreadCountResponse{Count: count}, nil
}

func (s *NotificationService) MarkNotificationRead(ctx context.Context, req *v1.MarkNotificationReadRequest) (*v1.MarkNotificationReadResponse, error) {
	value, err := s.uc.MarkRead(ctx, identity.Subject(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &v1.MarkNotificationReadResponse{Notification: toProto(value)}, nil
}

func (s *NotificationService) MarkAllNotificationsRead(ctx context.Context, _ *v1.MarkAllNotificationsReadRequest) (*v1.MarkAllNotificationsReadResponse, error) {
	updated, err := s.uc.MarkAllRead(ctx, identity.Subject(ctx))
	if err != nil {
		return nil, err
	}
	return &v1.MarkAllNotificationsReadResponse{Updated: updated}, nil
}

func paginate(page, size int32) (int64, int32) {
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return int64(page) * int64(size), size
}
