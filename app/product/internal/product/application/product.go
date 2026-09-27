package application

import (
	"context"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
)

// Commands 负责商品写用例；简单查询由入站适配器直接调用 domain.Reader。
type Commands struct{ repo domain.Repository }

func NewCommands(repo domain.Repository) *Commands { return &Commands{repo: repo} }

func (c *Commands) Create(ctx context.Context, params domain.NewProductParams) (*domain.Product, error) {
	product, err := domain.NewProduct(params)
	if err != nil {
		return nil, err
	}
	return c.repo.Create(ctx, product)
}

func (c *Commands) Update(ctx context.Context, id int64, params domain.UpdateProductParams) (*domain.Product, error) {
	product, err := c.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := product.Update(params); err != nil {
		return nil, err
	}
	return c.repo.Update(ctx, product)
}

func (c *Commands) Delete(ctx context.Context, id int64) error { return c.repo.Delete(ctx, id) }
