package infrastructure

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	eventv1 "github.com/eagle-go/eagle/api/eagle/event/v1"
	"github.com/eagle-go/eagle/app/order/internal/order/domain"
)

func TestRepositoryCreatesAggregateAtomicallyAndScopesOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	value, err := domain.New("owner-1", "request-1", []domain.RequestedItem{{ProductID: 7, Quantity: 2}}, map[int64]domain.ProductSnapshot{
		7: {ID: 7, SKU: "SKU-7", Name: "Demo", PriceCents: 500, Active: true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	repo := NewRepository(testDB)
	created, err := repo.Create(context.Background(), value)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var eventID string
	var payload []byte
	if err := testDB.SQL().QueryRowContext(context.Background(), `
SELECT id, payload FROM event_outbox WHERE aggregate_id = $1 AND event_type = 'eagle.event.v1.OrderCreatedV1'`, created.ID()).Scan(&eventID, &payload); err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if eventID == created.ID() {
		t.Fatal("event id must be independent from aggregate id")
	}
	var envelope eventv1.EventEnvelope
	if err := proto.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.GetEventId() != eventID || envelope.GetAggregateId() != created.ID() ||
		envelope.GetSchemaVersion() != 1 || envelope.GetProducer() != "order" {
		t.Fatalf("envelope = %+v", &envelope)
	}
	var event eventv1.OrderCreatedV1
	if err := proto.Unmarshal(envelope.GetPayload(), &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.GetEventId() != eventID || event.GetOrderId() != created.ID() || event.GetTotalCents() != 1000 {
		t.Fatalf("event = %+v", &event)
	}
	got, err := repo.GetOwned(context.Background(), "owner-1", created.ID())
	if err != nil || got.TotalCents() != 1000 || len(got.Items()) != 1 {
		t.Fatalf("GetOwned = %+v, %v", got, err)
	}
	if _, err := repo.GetOwned(context.Background(), "owner-2", created.ID()); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("cross-owner error = %v", err)
	}
	values, total, err := repo.ListOwned(context.Background(), domain.ListQuery{OwnerSubject: "owner-1", PageSize: 20})
	if err != nil || total != 1 || len(values) != 1 || len(values[0].Items()) != 1 {
		t.Fatalf("ListOwned = %d/%d, %v", len(values), total, err)
	}
	idempotent, err := repo.Create(context.Background(), value)
	if err != nil || idempotent.ID() != created.ID() {
		t.Fatalf("idempotent Create = %v, %v", idempotent, err)
	}
}

func TestConcurrentCreationDeduplicatesAndRejectsDifferentIntent(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	repo := NewRepository(testDB)
	products := map[int64]domain.ProductSnapshot{8: {ID: 8, SKU: "SKU-8", Name: "Concurrent", PriceCents: 100, Active: true}}
	const workers = 8
	results := make(chan *domain.Order, workers)
	failures := make(chan error, workers)
	start := make(chan struct{})
	for range workers {
		go func() {
			<-start
			value, err := domain.New("concurrent-owner", "same-key", []domain.RequestedItem{{ProductID: 8, Quantity: 1}}, products)
			if err == nil {
				value, err = repo.Create(ctx, value)
			}
			results <- value
			failures <- err
		}()
	}
	close(start)
	var id string
	for range workers {
		value := <-results
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		if id == "" {
			id = value.ID()
		}
		if id != value.ID() {
			t.Fatalf("duplicate orders %s and %s", id, value.ID())
		}
	}
	var count int
	if err := testDB.SQL().QueryRowContext(ctx, "SELECT count(*) FROM event_outbox WHERE aggregate_id = $1", id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("outbox count=%d, %v", count, err)
	}
	conflict, err := domain.New("concurrent-owner", "same-key", []domain.RequestedItem{{ProductID: 8, Quantity: 2}}, products)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, conflict); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("different intent: %v", err)
	}
	// Old binaries write an empty fingerprint during the rolling migration window.
	if _, err := testDB.SQL().ExecContext(ctx, "UPDATE purchase_order SET request_fingerprint = '' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	existing, err := repo.FindCreated(ctx, "concurrent-owner", "same-key")
	if err != nil || existing.RequestFingerprint() == "" {
		t.Fatalf("legacy recovery: %v, %v", existing, err)
	}
	if _, err := repo.FindCreated(ctx, "another-owner", "same-key"); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("owner boundary: %v", err)
	}
}
