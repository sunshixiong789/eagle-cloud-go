package infrastructure

import (
	"context"
	"fmt"

	platformdb "github.com/eagle-go/eagle/app/product/internal/platform/database"
	"github.com/eagle-go/eagle/app/product/internal/platform/database/ent"
	"github.com/eagle-go/eagle/app/product/internal/platform/database/ent/product"
	"github.com/eagle-go/eagle/app/product/internal/product/domain"
)

type repository struct{ db *platformdb.Database }

func NewRepository(db *platformdb.Database) domain.Repository { return &repository{db: db} }

func toDomain(row *ent.Product) (*domain.Product, error) {
	if row == nil {
		return nil, nil
	}
	value, err := domain.RehydrateProduct(domain.ProductSnapshot{
		ID: row.ID, SKU: row.Sku, Name: row.Name, Description: row.Description,
		PriceCents: row.PriceCents, Active: row.Active,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("rehydrate product %d: %w", row.ID, err)
	}
	return value, nil
}

func (r *repository) Create(ctx context.Context, value *domain.Product) (*domain.Product, error) {
	row, err := r.db.Client().Product.Create().
		SetSku(value.SKU()).SetName(value.Name()).SetDescription(value.Description()).
		SetPriceCents(value.PriceCents()).SetActive(value.Active()).Save(ctx)
	if err != nil {
		if platformdb.IsUniqueViolation(err) {
			return nil, domain.ErrProductSKUDuplicated
		}
		return nil, fmt.Errorf("create product: %w", err)
	}
	return toDomain(row)
}

func (r *repository) Get(ctx context.Context, id int64) (*domain.Product, error) {
	row, err := r.db.Client().Product.Get(ctx, id)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrProductNotFound
		}
		return nil, fmt.Errorf("get product: %w", err)
	}
	return toDomain(row)
}

func (r *repository) BatchGet(ctx context.Context, ids []int64) ([]*domain.Product, error) {
	rows, err := r.db.Client().Product.Query().Where(product.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("batch get products: %w", err)
	}
	out := make([]*domain.Product, 0, len(rows))
	for _, row := range rows {
		value, err := toDomain(row)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func (r *repository) List(ctx context.Context, q domain.ListQuery) ([]*domain.Product, int64, error) {
	query := r.db.Client().Product.Query()
	if q.ActiveOnly {
		query = query.Where(product.ActiveEQ(true))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}
	rows, err := query.Order(ent.Desc(product.FieldCreatedAt)).Offset(int(q.Offset)).Limit(int(q.PageSize)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	out := make([]*domain.Product, 0, len(rows))
	for _, row := range rows {
		value, err := toDomain(row)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, value)
	}
	return out, int64(total), nil
}

func (r *repository) Update(ctx context.Context, value *domain.Product) (*domain.Product, error) {
	row, err := r.db.Client().Product.UpdateOneID(value.ID()).
		SetName(value.Name()).SetDescription(value.Description()).
		SetPriceCents(value.PriceCents()).SetActive(value.Active()).Save(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrProductNotFound
		}
		return nil, fmt.Errorf("update product: %w", err)
	}
	return toDomain(row)
}

func (r *repository) Delete(ctx context.Context, id int64) error {
	if err := r.db.Client().Product.DeleteOneID(id).Exec(ctx); err != nil {
		if platformdb.IsNotFound(err) {
			return domain.ErrProductNotFound
		}
		return fmt.Errorf("delete product: %w", err)
	}
	return nil
}
