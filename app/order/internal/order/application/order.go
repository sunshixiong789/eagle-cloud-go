package application

import (
	"context"

	"github.com/eagle-go/eagle/app/order/internal/order/domain"
)

type Usecase struct {
	repo     domain.Writer
	products domain.ProductCatalog
}

func NewUsecase(repo domain.Writer, products domain.ProductCatalog) *Usecase {
	return &Usecase{repo: repo, products: products}
}

func (uc *Usecase) Create(ctx context.Context, owner, idempotencyKey string, requested []domain.RequestedItem) (*domain.Order, error) {
	ids := make([]int64, 0, len(requested))
	seen := make(map[int64]struct{}, len(requested))
	for _, item := range requested {
		if _, ok := seen[item.ProductID]; !ok {
			seen[item.ProductID] = struct{}{}
			ids = append(ids, item.ProductID)
		}
	}
	products, err := uc.products.BatchGet(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]domain.ProductSnapshot, len(products))
	for _, product := range products {
		byID[product.ID] = product
	}
	order, err := domain.New(owner, idempotencyKey, requested, byID)
	if err != nil {
		return nil, err
	}
	return uc.repo.Create(ctx, order)
}
