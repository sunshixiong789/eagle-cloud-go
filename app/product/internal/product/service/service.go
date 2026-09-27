package service

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	"github.com/eagle-go/eagle/app/product/internal/product/application"
	"github.com/eagle-go/eagle/app/product/internal/product/domain"
)

type ProductService struct {
	productv1.UnimplementedProductServiceServer
	commands *application.Commands
	reader   domain.Reader
}

func NewProductService(commands *application.Commands, reader domain.Reader) *ProductService {
	return &ProductService{commands: commands, reader: reader}
}

func toProto(value *domain.Product) *productv1.Product {
	if value == nil {
		return nil
	}
	return &productv1.Product{
		Id: value.ID(), Sku: value.SKU(), Name: value.Name(), Description: value.Description(),
		PriceCents: value.PriceCents(), Active: value.Active(),
		CreatedAt: timestamppb.New(value.CreatedAt()), UpdatedAt: timestamppb.New(value.UpdatedAt()),
	}
}

func (s *ProductService) CreateProduct(ctx context.Context, req *productv1.CreateProductRequest) (*productv1.CreateProductResponse, error) {
	value, err := s.commands.Create(ctx, domain.NewProductParams{
		SKU: req.GetSku(), Name: req.GetName(), Description: req.GetDescription(),
		PriceCents: req.GetPriceCents(), Active: req.GetActive(),
	})
	if err != nil {
		return nil, err
	}
	return &productv1.CreateProductResponse{Product: toProto(value)}, nil
}

func (s *ProductService) GetProduct(ctx context.Context, req *productv1.GetProductRequest) (*productv1.GetProductResponse, error) {
	value, err := s.reader.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &productv1.GetProductResponse{Product: toProto(value)}, nil
}

func (s *ProductService) BatchGetProducts(ctx context.Context, req *productv1.BatchGetProductsRequest) (*productv1.BatchGetProductsResponse, error) {
	values, err := s.reader.BatchGet(ctx, req.GetIds())
	if err != nil {
		return nil, err
	}
	out := make([]*productv1.Product, 0, len(values))
	for _, value := range values {
		out = append(out, toProto(value))
	}
	return &productv1.BatchGetProductsResponse{Products: out}, nil
}

func (s *ProductService) ListProducts(ctx context.Context, req *productv1.ListProductsRequest) (*productv1.ListProductsResponse, error) {
	offset, limit := paginate(req.GetPage(), req.GetPageSize())
	values, total, err := s.reader.List(ctx, domain.ListQuery{Offset: offset, PageSize: limit, ActiveOnly: req.GetActiveOnly()})
	if err != nil {
		return nil, err
	}
	out := make([]*productv1.Product, 0, len(values))
	for _, value := range values {
		out = append(out, toProto(value))
	}
	return &productv1.ListProductsResponse{Products: out, Total: total}, nil
}

func (s *ProductService) UpdateProduct(ctx context.Context, req *productv1.UpdateProductRequest) (*productv1.UpdateProductResponse, error) {
	value, err := s.commands.Update(ctx, req.GetId(), domain.UpdateProductParams{
		Name: req.GetName(), Description: req.GetDescription(),
		PriceCents: req.GetPriceCents(), Active: req.GetActive(),
	})
	if err != nil {
		return nil, err
	}
	return &productv1.UpdateProductResponse{Product: toProto(value)}, nil
}

func (s *ProductService) DeleteProduct(ctx context.Context, req *productv1.DeleteProductRequest) (*productv1.DeleteProductResponse, error) {
	if err := s.commands.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &productv1.DeleteProductResponse{}, nil
}

func paginate(page, size int32) (int64, int32) {
	if size == 0 {
		size = 20
	}
	return int64(page) * int64(size), size
}
