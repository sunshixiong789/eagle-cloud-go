package application

import (
	"context"
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
