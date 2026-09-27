package service

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	orderv1 "github.com/eagle-go/eagle/api/eagle/order/v1"
	"github.com/eagle-go/eagle/app/order/internal/order/application"
	"github.com/eagle-go/eagle/app/order/internal/order/domain"
	"github.com/eagle-go/eagle/pkg/identity"
)

type OrderService struct {
	orderv1.UnimplementedOrderServiceServer
	commands *application.Usecase
	reader   domain.Reader
}

func NewOrderService(commands *application.Usecase, reader domain.Reader) *OrderService {
	return &OrderService{commands: commands, reader: reader}
}

func toProto(value *domain.Order) *orderv1.Order {
	if value == nil {
		return nil
	}
	items := value.Items()
	out := make([]*orderv1.OrderItem, 0, len(items))
	for _, item := range items {
		out = append(out, &orderv1.OrderItem{
			ProductId: item.ProductID, ProductSku: item.ProductSKU, ProductName: item.ProductName,
			UnitPriceCents: item.UnitPriceCents, Quantity: item.Quantity, SubtotalCents: item.SubtotalCents,
		})
	}
	return &orderv1.Order{Id: value.ID(), Status: orderv1.OrderStatus_ORDER_STATUS_CREATED, TotalCents: value.TotalCents(), Items: out, CreatedAt: timestamppb.New(value.CreatedAt())}
}

func (s *OrderService) CreateOrder(ctx context.Context, req *orderv1.CreateOrderRequest) (*orderv1.CreateOrderResponse, error) {
	requested := make([]domain.RequestedItem, 0, len(req.GetItems()))
	for _, item := range req.GetItems() {
		requested = append(requested, domain.RequestedItem{ProductID: item.GetProductId(), Quantity: item.GetQuantity()})
	}
	value, err := s.commands.Create(ctx, identity.Subject(ctx), req.GetIdempotencyKey(), requested)
	if err != nil {
		return nil, err
	}
	return &orderv1.CreateOrderResponse{Order: toProto(value)}, nil
}

func (s *OrderService) GetMyOrder(ctx context.Context, req *orderv1.GetMyOrderRequest) (*orderv1.GetMyOrderResponse, error) {
	value, err := s.reader.GetOwned(ctx, identity.Subject(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &orderv1.GetMyOrderResponse{Order: toProto(value)}, nil
}

func (s *OrderService) ListMyOrders(ctx context.Context, req *orderv1.ListMyOrdersRequest) (*orderv1.ListMyOrdersResponse, error) {
	pageSize := req.GetPageSize()
	if pageSize == 0 {
		pageSize = 20
	}
	values, total, err := s.reader.ListOwned(ctx, domain.ListQuery{
		OwnerSubject: identity.Subject(ctx),
		Offset:       int64(req.GetPage()) * int64(pageSize),
		PageSize:     pageSize,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*orderv1.Order, 0, len(values))
	for _, value := range values {
		out = append(out, toProto(value))
	}
	return &orderv1.ListMyOrdersResponse{Orders: out, Total: total}, nil
}
