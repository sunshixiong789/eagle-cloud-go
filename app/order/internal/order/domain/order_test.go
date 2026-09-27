package domain

import (
	"errors"
	"testing"
)

func TestNewOrderSnapshotsPriceAndRejectsDuplicates(t *testing.T) {
	products := map[int64]ProductSnapshot{1: {ID: 1, SKU: "SKU-1", Name: "demo", PriceCents: 2500, Active: true}}
	order, err := New("subject", "request-1", []RequestedItem{{ProductID: 1, Quantity: 2}}, products)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if order.TotalCents() != 5000 || order.Items()[0].UnitPriceCents != 2500 {
		t.Fatalf("unexpected price snapshot: %+v", order)
	}
	_, err = New("subject", "request-2", []RequestedItem{{ProductID: 1, Quantity: 1}, {ProductID: 1, Quantity: 1}}, products)
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestNewOrderRejectsInactiveProduct(t *testing.T) {
	_, err := New("subject", "request-1", []RequestedItem{{ProductID: 1, Quantity: 1}}, map[int64]ProductSnapshot{
		1: {ID: 1, PriceCents: 100, Active: false},
	})
	if !errors.Is(err, ErrProductUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewOrderRejectsOverflow(t *testing.T) {
	_, err := New("subject", "request-1", []RequestedItem{{ProductID: 1, Quantity: 4}}, map[int64]ProductSnapshot{
		1: {ID: 1, SKU: "SKU", Name: "name", PriceCents: 1<<62 + 1, Active: true},
	})
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidOrder)
	}
}

func TestRehydrateOrderRejectsOverflowedSubtotal(t *testing.T) {
	_, err := RehydrateOrder(OrderSnapshot{
		ID: "order", OwnerSubject: "owner", IdempotencyKey: "request", Status: StatusCreated,
		TotalCents: 4,
		Items: []Item{{
			ProductID: 1, ProductSKU: "SKU", ProductName: "name",
			UnitPriceCents: 1<<62 + 1, Quantity: 4, SubtotalCents: 4,
		}},
	})
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidOrder)
	}
}
