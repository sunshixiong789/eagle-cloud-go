package application

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/app/order/internal/order/domain"
)

type stubProducts struct {
	ids      []int64
	products []domain.ProductSnapshot
}

func (s *stubProducts) BatchGet(_ context.Context, ids []int64) ([]domain.ProductSnapshot, error) {
	s.ids = append([]int64(nil), ids...)
	return s.products, nil
}

type stubOrders struct{ created *domain.Order }

func (s *stubOrders) FindCreated(context.Context, string, string) (*domain.Order, error) {
	if s.created == nil {
		return nil, domain.ErrOrderNotFound
	}
	return s.created, nil
}

func (s *stubOrders) Create(_ context.Context, value *domain.Order) (*domain.Order, error) {
	s.created = value
	return value, nil
}

func TestCreateReadsProductsThroughPortAndPersistsSnapshot(t *testing.T) {
	products := &stubProducts{products: []domain.ProductSnapshot{{ID: 3, SKU: "SKU-3", Name: "Demo", PriceCents: 800, Active: true}}}
	orders := &stubOrders{}
	value, err := NewUsecase(orders, products).Create(context.Background(), "owner", "request-1", []domain.RequestedItem{{ProductID: 3, Quantity: 2}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(products.ids) != 1 || products.ids[0] != 3 {
		t.Fatalf("requested product ids = %v", products.ids)
	}
	if orders.created != value || value.TotalCents() != 1600 || value.Items()[0].ProductSKU != "SKU-3" {
		t.Fatalf("persisted order = %+v", value)
	}
}

func TestCreateRecoversCompletedOrderWithoutProductDependency(t *testing.T) {
	products := &stubProducts{products: []domain.ProductSnapshot{{ID: 3, SKU: "SKU-3", Name: "Demo", PriceCents: 800, Active: true}}}
	orders := &stubOrders{}
	uc := NewUsecase(orders, products)
	request := []domain.RequestedItem{{ProductID: 3, Quantity: 2}}
	first, err := uc.Create(context.Background(), "owner", "retry", request)
	if err != nil {
		t.Fatal(err)
	}
	products.products = nil // A deleted/unavailable product must not prevent recovery.
	products.ids = nil
	recovered, err := uc.Create(context.Background(), "owner", "retry", request)
	if err != nil || recovered.ID() != first.ID() || len(products.ids) != 0 {
		t.Fatalf("recovery: %v, %v, upstream calls=%v", recovered, err, products.ids)
	}
	_, err = uc.Create(context.Background(), "owner", "retry", []domain.RequestedItem{{ProductID: 3, Quantity: 1}})
	if !errors.Is(err, domain.ErrIdempotencyConflict) || len(products.ids) != 0 {
		t.Fatalf("conflict: %v", err)
	}
}
