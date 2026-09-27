// Package application coordinates notification use cases.
package application

import (
	"context"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/notification/domain"
)

type Usecase struct{ repo domain.Repository }

func NewUsecase(repo domain.Repository) *Usecase { return &Usecase{repo: repo} }

func (uc *Usecase) Send(ctx context.Context, sender, recipient, title, content string) (*domain.Notification, error) {
	return uc.repo.Create(ctx, &domain.Notification{
		SenderSubject: sender, RecipientSubject: recipient,
		Title: title, Content: content,
	})
}

func (uc *Usecase) ReceiveOrderCreated(ctx context.Context, eventID, recipient, orderID string, totalCents int64) error {
	if eventID == "" || recipient == "" || orderID == "" || totalCents <= 0 {
		return fmt.Errorf("notification: invalid order-created event")
	}
	_, err := uc.repo.CreateFromEvent(ctx, eventID, "eagle.event.v1.OrderCreatedV1", &domain.Notification{
		RecipientSubject: recipient,
		Title:            "订单创建成功",
		Content:          fmt.Sprintf("订单 %s 已创建，金额 ¥%.2f", orderID, float64(totalCents)/100),
	})
	return err
}

func (uc *Usecase) List(ctx context.Context, q domain.ListQuery) ([]*domain.Notification, int64, error) {
	return uc.repo.List(ctx, q)
}

func (uc *Usecase) UnreadCount(ctx context.Context, recipient string) (int64, error) {
	return uc.repo.UnreadCount(ctx, recipient)
}

func (uc *Usecase) MarkRead(ctx context.Context, recipient string, id int64) (*domain.Notification, error) {
	return uc.repo.MarkRead(ctx, recipient, id, time.Now())
}

func (uc *Usecase) MarkAllRead(ctx context.Context, recipient string) (int64, error) {
	return uc.repo.MarkAllRead(ctx, recipient, time.Now())
}
