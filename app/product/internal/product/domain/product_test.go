package domain

import (
	"errors"
	"testing"
)

func TestProductGuardsInvariants(t *testing.T) {
	if _, err := NewProduct(NewProductParams{Name: "missing sku", PriceCents: 1}); !errors.Is(err, ErrInvalidProduct) {
		t.Fatalf("NewProduct error = %v", err)
	}
	product, err := NewProduct(NewProductParams{SKU: "SKU-1", Name: "Demo", PriceCents: 100, Active: true})
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if err := product.Update(UpdateProductParams{Name: "Demo", PriceCents: 0}); !errors.Is(err, ErrInvalidProduct) {
		t.Fatalf("Update error = %v", err)
	}
	if product.PriceCents() != 100 || !product.Active() {
		t.Fatalf("failed update mutated product: price=%d active=%v", product.PriceCents(), product.Active())
	}
}
