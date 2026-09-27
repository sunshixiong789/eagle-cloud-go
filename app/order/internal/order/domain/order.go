package domain

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrOrderNotFound      = errors.New("order not found")
	ErrProductUnavailable = errors.New("product unavailable")
	ErrInvalidOrder       = errors.New("invalid order")
)

const StatusCreated = "created"

type ProductSnapshot struct {
	ID         int64
	SKU        string
	Name       string
	PriceCents int64
	Active     bool
}

type RequestedItem struct {
	ProductID int64
	Quantity  int32
}

type Item struct {
	ProductID      int64
	ProductSKU     string
	ProductName    string
	UnitPriceCents int64
	Quantity       int32
	SubtotalCents  int64
}

type Order struct {
	id             string
	ownerSubject   string
	idempotencyKey string
	status         string
	totalCents     int64
	items          []Item
	createdAt      time.Time
}

func New(owner, idempotencyKey string, requests []RequestedItem, products map[int64]ProductSnapshot) (*Order, error) {
	if owner == "" || idempotencyKey == "" || len(idempotencyKey) > 64 || len(requests) == 0 {
		return nil, ErrInvalidOrder
	}
	seen := make(map[int64]struct{}, len(requests))
	items := make([]Item, 0, len(requests))
	var total int64
	for _, requested := range requests {
		if requested.ProductID <= 0 || requested.Quantity <= 0 {
			return nil, ErrInvalidOrder
		}
		if _, exists := seen[requested.ProductID]; exists {
			return nil, fmt.Errorf("%w: duplicate product %d", ErrInvalidOrder, requested.ProductID)
		}
		seen[requested.ProductID] = struct{}{}
		product, ok := products[requested.ProductID]
		if !ok || !product.Active || product.ID != requested.ProductID || product.PriceCents <= 0 ||
			strings.TrimSpace(product.SKU) == "" || strings.TrimSpace(product.Name) == "" {
			return nil, fmt.Errorf("%w: product %d", ErrProductUnavailable, requested.ProductID)
		}
		if product.PriceCents > math.MaxInt64/int64(requested.Quantity) {
			return nil, fmt.Errorf("%w: total overflow", ErrInvalidOrder)
		}
		subtotal := product.PriceCents * int64(requested.Quantity)
		if total > math.MaxInt64-subtotal {
			return nil, fmt.Errorf("%w: total overflow", ErrInvalidOrder)
		}
		total += subtotal
		items = append(items, Item{
			ProductID: product.ID, ProductSKU: product.SKU, ProductName: product.Name,
			UnitPriceCents: product.PriceCents, Quantity: requested.Quantity, SubtotalCents: subtotal,
		})
	}
	id, err := NewID()
	if err != nil {
		return nil, fmt.Errorf("generate order id: %w", err)
	}
	return &Order{
		id: id, ownerSubject: owner, idempotencyKey: idempotencyKey,
		status: StatusCreated, totalCents: total, items: items,
	}, nil
}

type OrderSnapshot struct {
	ID             string
	OwnerSubject   string
	IdempotencyKey string
	Status         string
	TotalCents     int64
	Items          []Item
	CreatedAt      time.Time
}

func RehydrateOrder(snapshot OrderSnapshot) (*Order, error) {
	if snapshot.ID == "" || snapshot.OwnerSubject == "" || snapshot.IdempotencyKey == "" || len(snapshot.IdempotencyKey) > 64 ||
		snapshot.Status != StatusCreated || snapshot.TotalCents <= 0 || len(snapshot.Items) == 0 {
		return nil, ErrInvalidOrder
	}
	var total int64
	seen := make(map[int64]struct{}, len(snapshot.Items))
	for _, item := range snapshot.Items {
		if item.ProductID <= 0 || strings.TrimSpace(item.ProductSKU) == "" || strings.TrimSpace(item.ProductName) == "" ||
			item.UnitPriceCents <= 0 || item.Quantity <= 0 || item.UnitPriceCents > math.MaxInt64/int64(item.Quantity) ||
			item.SubtotalCents != item.UnitPriceCents*int64(item.Quantity) || total > math.MaxInt64-item.SubtotalCents {
			return nil, ErrInvalidOrder
		}
		if _, exists := seen[item.ProductID]; exists {
			return nil, ErrInvalidOrder
		}
		seen[item.ProductID] = struct{}{}
		total += item.SubtotalCents
	}
	if total != snapshot.TotalCents {
		return nil, ErrInvalidOrder
	}
	items := append([]Item(nil), snapshot.Items...)
	return &Order{
		id: snapshot.ID, ownerSubject: snapshot.OwnerSubject, idempotencyKey: snapshot.IdempotencyKey,
		status: snapshot.Status, totalCents: snapshot.TotalCents, items: items, createdAt: snapshot.CreatedAt,
	}, nil
}

func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func (o *Order) ID() string             { return o.id }
func (o *Order) OwnerSubject() string   { return o.ownerSubject }
func (o *Order) IdempotencyKey() string { return o.idempotencyKey }
func (o *Order) Status() string         { return o.status }
func (o *Order) TotalCents() int64      { return o.totalCents }
func (o *Order) Items() []Item          { return append([]Item(nil), o.items...) }
func (o *Order) CreatedAt() time.Time   { return o.createdAt }

type ListQuery struct {
	OwnerSubject string
	Offset       int64
	PageSize     int32
}

type Writer interface {
	Create(context.Context, *Order) (*Order, error)
}

type Reader interface {
	GetOwned(context.Context, string, string) (*Order, error)
	ListOwned(context.Context, ListQuery) ([]*Order, int64, error)
}

type Repository interface {
	Writer
	Reader
}

type ProductCatalog interface {
	BatchGet(context.Context, []int64) ([]ProductSnapshot, error)
}
