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
		r.invalidate(ctx, created.ID())
	}
	return created, err
}

func (r *cachedRepository) Get(ctx context.Context, id int64) (*domain.Product, error) {
	cached, token, cacheErr := r.cache.Lookup(ctx, id)
	if cacheErr == nil && cached != nil {
		return cached, nil
	}
	if cacheErr != nil {
		r.warn(ctx, "读取 Redis 商品缓存失败", cacheErr)
	}
	product, err := r.next.Get(ctx, id)
	if err == nil && cacheErr == nil && token != "" {
		if fillErr := r.cache.Fill(ctx, product, token); fillErr != nil {
			r.warn(ctx, "写入 Redis 商品缓存失败", fillErr)
		}
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
		r.invalidate(ctx, updated.ID())
	}
	return updated, err
}

func (r *cachedRepository) Delete(ctx context.Context, id int64) error {
	if err := r.next.Delete(ctx, id); err != nil {
		return err
	}
	r.invalidate(ctx, id)
	return nil
}

func (r *cachedRepository) invalidate(ctx context.Context, id int64) {
	// The database has committed. Give invalidation its own bounded cache timeout
	// even if the HTTP client disconnected after that commit.
	if err := r.cache.Invalidate(context.WithoutCancel(ctx), id); err != nil {
		r.warn(ctx, "失效 Redis 商品缓存失败", err)
	}
}

func (r *cachedRepository) warn(ctx context.Context, message string, err error) {
	if r.logger != nil {
		r.logger.WarnContext(ctx, message, "error", err)
	}
}
