package infrastructure

import (
	"context"
	"log/slog"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
)

// cachedRepository keeps Redis outside the domain port and fails open to PostgreSQL.
type cachedRepository struct {
	next   domain.Repository
	cache  productCache
	logger *slog.Logger
}

func NewCachedRepository(next domain.Repository, cache productCache, logger *slog.Logger) domain.Repository {
	return &cachedRepository{next: next, cache: cache, logger: logger}
}

func (r *cachedRepository) Create(ctx context.Context, value *domain.Product) (*domain.Product, error) {
	created, err := r.next.Create(ctx, value)
	if err == nil {
		r.setIfNewer(ctx, created)
	}
	return created, err
}

func (r *cachedRepository) Get(ctx context.Context, id int64) (*domain.Product, error) {
	if cached, ok, err := r.cache.Get(ctx, id); err == nil && ok {
		return cached, nil
	} else if err != nil {
		r.warn(ctx, "读取 Redis 商品缓存失败", err)
	}
	product, err := r.next.Get(ctx, id)
	if err == nil {
		r.setIfNewer(ctx, product)
	}
	return product, err
}

func (r *cachedRepository) BatchGet(ctx context.Context, ids []int64) ([]*domain.Product, error) {
	return r.next.BatchGet(ctx, ids)
}

func (r *cachedRepository) List(ctx context.Context, query domain.ListQuery) ([]*domain.Product, int64, error) {
	return r.next.List(ctx, query)
}

func (r *cachedRepository) Update(ctx context.Context, value *domain.Product) (*domain.Product, error) {
	updated, err := r.next.Update(ctx, value)
	if err == nil {
		r.setIfNewer(ctx, updated)
	}
	return updated, err
}

func (r *cachedRepository) Delete(ctx context.Context, id int64) error {
	if err := r.next.Delete(ctx, id); err != nil {
		return err
	}
	if err := r.cache.Delete(ctx, id); err != nil {
		r.warn(ctx, "删除 Redis 商品缓存失败", err)
	}
	return nil
}

func (r *cachedRepository) setIfNewer(ctx context.Context, product *domain.Product) {
	if err := r.cache.SetIfNewer(ctx, product); err != nil {
		r.warn(ctx, "写入 Redis 商品缓存失败", err)
	}
}

func (r *cachedRepository) warn(ctx context.Context, message string, err error) {
	if r.logger != nil {
		r.logger.WarnContext(ctx, message, "error", err)
	}
}
