package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
)

func TestRepositoryLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	repo := NewRepository(testDB)
	product, err := domain.NewProduct(domain.NewProductParams{SKU: "DEMO-1", Name: "Demo", PriceCents: 1999, Active: true})
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	created, err := repo.Create(context.Background(), product)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID() == 0 {
		t.Fatal("missing generated product id")
	}
	duplicate, err := domain.NewProduct(domain.NewProductParams{SKU: "DEMO-1", Name: "Duplicate", PriceCents: 100, Active: true})
	if err != nil {
		t.Fatalf("NewProduct duplicate: %v", err)
	}
	if _, err := repo.Create(context.Background(), duplicate); !errors.Is(err, domain.ErrProductSKUDuplicated) {
		t.Fatalf("duplicate error = %v", err)
	}
	if err := created.Update(domain.UpdateProductParams{Name: "Updated", PriceCents: created.PriceCents(), Active: true}); err != nil {
		t.Fatalf("Update domain: %v", err)
	}
	updated, err := repo.Update(context.Background(), created)
	if err != nil || updated.Name() != "Updated" {
		t.Fatalf("Update = %+v, %v", updated, err)
	}
	values, total, err := repo.List(context.Background(), domain.ListQuery{PageSize: 20, ActiveOnly: true})
	if err != nil || total != 1 || len(values) != 1 {
		t.Fatalf("List = %d/%d, %v", len(values), total, err)
	}
}
