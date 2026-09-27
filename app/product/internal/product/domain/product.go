package domain

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrProductSKUDuplicated = errors.New("product SKU already exists")
	ErrInvalidProduct       = errors.New("invalid product")
)

// Product 是商品聚合根。SKU 创建后不可变，所有可变字段只能通过领域方法更新。
type Product struct {
	id          int64
	sku         string
	name        string
	description string
	priceCents  int64
	active      bool
	createdAt   time.Time
	updatedAt   time.Time
}

type NewProductParams struct {
	SKU         string
	Name        string
	Description string
	PriceCents  int64
	Active      bool
}

type UpdateProductParams struct {
	Name        string
	Description string
	PriceCents  int64
	Active      bool
}

func NewProduct(params NewProductParams) (*Product, error) {
	product := &Product{
		sku: strings.TrimSpace(params.SKU), name: strings.TrimSpace(params.Name),
		description: params.Description, priceCents: params.PriceCents, active: params.Active,
	}
	if err := product.validate(); err != nil {
		return nil, err
	}
	return product, nil
}

func (p *Product) Update(params UpdateProductParams) error {
	next := *p
	next.name = strings.TrimSpace(params.Name)
	next.description = params.Description
	next.priceCents = params.PriceCents
	next.active = params.Active
	if err := next.validate(); err != nil {
		return err
	}
	*p = next
	return nil
}

func (p *Product) validate() error {
	if p.sku == "" || utf8.RuneCountInString(p.sku) > 64 ||
		p.name == "" || utf8.RuneCountInString(p.name) > 128 ||
		utf8.RuneCountInString(p.description) > 2048 || p.priceCents <= 0 {
		return ErrInvalidProduct
	}
	return nil
}

type ProductSnapshot struct {
	ID          int64
	SKU         string
	Name        string
	Description string
	PriceCents  int64
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RehydrateProduct 在命令和查询路径上都校验持久化数据；非法快照不伪装成合法聚合。
func RehydrateProduct(snapshot ProductSnapshot) (*Product, error) {
	if snapshot.ID <= 0 {
		return nil, ErrInvalidProduct
	}
	product := &Product{
		id: snapshot.ID, sku: snapshot.SKU, name: snapshot.Name, description: snapshot.Description,
		priceCents: snapshot.PriceCents, active: snapshot.Active,
		createdAt: snapshot.CreatedAt, updatedAt: snapshot.UpdatedAt,
	}
	if err := product.validate(); err != nil {
		return nil, err
	}
	return product, nil
}

func (p *Product) ID() int64            { return p.id }
func (p *Product) SKU() string          { return p.sku }
func (p *Product) Name() string         { return p.name }
func (p *Product) Description() string  { return p.description }
func (p *Product) PriceCents() int64    { return p.priceCents }
func (p *Product) Active() bool         { return p.active }
func (p *Product) CreatedAt() time.Time { return p.createdAt }
func (p *Product) UpdatedAt() time.Time { return p.updatedAt }

type ListQuery struct {
	Offset     int64
	PageSize   int32
	ActiveOnly bool
}

type Reader interface {
	Get(context.Context, int64) (*Product, error)
	BatchGet(context.Context, []int64) ([]*Product, error)
	List(context.Context, ListQuery) ([]*Product, int64, error)
}

type Writer interface {
	Create(context.Context, *Product) (*Product, error)
	Update(context.Context, *Product) (*Product, error)
	Delete(context.Context, int64) error
}

type Repository interface {
	Reader
	Writer
}
