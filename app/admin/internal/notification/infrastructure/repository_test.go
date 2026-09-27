package infrastructure

import (
	"context"
	"testing"

	"github.com/eagle-go/eagle/app/admin/internal/notification/domain"
)

func TestCreateFromEventIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	repository := NewRepository(notificationTestDB)
	value := &domain.Notification{RecipientSubject: "owner-1", Title: "order", Content: "created"}
	created, err := repository.CreateFromEvent(context.Background(), "event-1", "OrderCreatedV1", value)
	if err != nil || !created {
		t.Fatalf("first CreateFromEvent() = %v, %v", created, err)
	}
	created, err = repository.CreateFromEvent(context.Background(), "event-1", "OrderCreatedV1", value)
	if err != nil || created {
		t.Fatalf("duplicate CreateFromEvent() = %v, %v", created, err)
	}
	var count int
	if err := notificationTestDB.SQL().QueryRowContext(context.Background(), `
SELECT count(*) FROM notification WHERE recipient_subject = 'owner-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("notification count = %d, want 1", count)
	}
}
