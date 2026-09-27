package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
)

type fakeProductCache struct {
	value *domain.Product
	err   error
}

func (f *fakeProductCache) Get(context.Context, int64) (*domain.Product, bool, error) {
	return f.value, f.value != nil, f.err
}
func (f *fakeProductCache) SetIfNewer(_ context.Context, value *domain.Product) error {
	f.value = value
	return f.err
}
func (f *fakeProductCache) Delete(context.Context, int64) error { f.value = nil; return f.err }

type fakeProductRepository struct {
	getCalls int
	value    *domain.Product
}

func (f *fakeProductRepository) Create(context.Context, *domain.Product) (*domain.Product, error) {
	return nil, nil
}
func (f *fakeProductRepository) Get(context.Context, int64) (*domain.Product, error) {
	f.getCalls++
	return f.value, nil
}
func (f *fakeProductRepository) BatchGet(context.Context, []int64) ([]*domain.Product, error) {
	return nil, nil
}
func (f *fakeProductRepository) List(context.Context, domain.ListQuery) ([]*domain.Product, int64, error) {
	return nil, 0, nil
}
func (f *fakeProductRepository) Update(context.Context, *domain.Product) (*domain.Product, error) {
	return nil, nil
}
func (f *fakeProductRepository) Delete(context.Context, int64) error { return nil }

func TestCachedRepositoryFallsBackAndWarmsCache(t *testing.T) {
	value, err := domain.RehydrateProduct(domain.ProductSnapshot{ID: 7, SKU: "cached", Name: "cached", PriceCents: 1})
	if err != nil {
		t.Fatalf("RehydrateProduct: %v", err)
	}
	repository := &fakeProductRepository{value: value}
	cache := &fakeProductCache{err: errors.New("redis unavailable")}
	wrapped := NewCachedRepository(repository, cache, nil)
	got, err := wrapped.Get(context.Background(), 7)
	if err != nil || got.ID() != 7 || repository.getCalls != 1 {
		t.Fatalf("Get() = %#v, %v, DB calls=%d", got, err, repository.getCalls)
	}
}
