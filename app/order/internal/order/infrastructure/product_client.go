package infrastructure

import (
	"context"
	"fmt"
	"time"

	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	"github.com/eagle-go/eagle/app/order/internal/order/domain"
	"github.com/eagle-go/eagle/pkg/authn"
	platformclient "github.com/eagle-go/eagle/pkg/platform/client"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/retryx"
)

type ProductClient struct {
	client      productv1.ProductServiceClient
	maxAttempts int
	backoff     time.Duration
}

func NewProductClient(c *config.Upstream, serviceAuth *config.ServiceAuth) (*ProductClient, func(), error) {
	credentials, err := authn.NewClientCredentials(authn.ClientCredentialsConfig{
		TokenURL: serviceAuth.GetTokenUrl(), ClientID: serviceAuth.GetClientId(), ClientSecret: serviceAuth.GetClientSecret(),
	})
	if err != nil {
		return nil, nil, err
	}
	middlewares, err := platformclient.NewMiddlewares(credentials.Client())
	if err != nil {
		return nil, nil, err
	}
	conn, err := kratosgrpc.NewClient(
		context.Background(),
		kratosgrpc.WithEndpoint(c.GetProductEndpoint()),
		kratosgrpc.WithTimeout(c.GetTimeout().AsDuration()),
		kratosgrpc.WithMiddleware(middlewares...),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect product service: %w", err)
	}
	return &ProductClient{
		client: productv1.NewProductServiceClient(conn), maxAttempts: int(c.GetMaxAttempts()),
		backoff: c.GetRetryBackoff().AsDuration(),
	}, func() { _ = conn.Close() }, nil
}

func (c *ProductClient) BatchGet(ctx context.Context, ids []int64) ([]domain.ProductSnapshot, error) {
	var resp *productv1.BatchGetProductsResponse
	err := retryx.Do(ctx, c.maxAttempts, c.backoff, retryableProductCall, func() error {
		var err error
		resp, err = c.client.BatchGetProducts(ctx, &productv1.BatchGetProductsRequest{Ids: ids})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("batch get products: %w", err)
	}
	out := make([]domain.ProductSnapshot, 0, len(resp.GetProducts()))
	for _, product := range resp.GetProducts() {
		out = append(out, domain.ProductSnapshot{
			ID: product.GetId(), SKU: product.GetSku(), Name: product.GetName(),
			PriceCents: product.GetPriceCents(), Active: product.GetActive(),
		})
	}
	return out, nil
}

func retryableProductCall(err error) bool {
	code := status.Code(err)
	return code == codes.Unavailable || code == codes.ResourceExhausted
}
