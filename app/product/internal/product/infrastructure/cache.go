package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/rueidis"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type productCache interface {
	Get(context.Context, int64) (*domain.Product, bool, error)
	SetIfNewer(context.Context, *domain.Product) error
	Delete(context.Context, int64) error
}

type RedisProductCache struct {
	client rueidis.Client
	ttl    time.Duration
}

func NewRedisProductCache(config *config.Cache_Redis) (*RedisProductCache, func(), error) {
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{config.GetAddress()}, Username: config.GetUsername(), Password: config.GetPassword(),
		SelectDB: int(config.GetDatabase()), ClientName: "eagle-product",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("connect redis: %w", err)
	}
	return &RedisProductCache{client: client, ttl: config.GetTtl().AsDuration()}, client.Close, nil
}

func productCacheKey(id int64) string { return "eagle:product:v2:" + strconv.FormatInt(id, 10) }

type productCacheRecord struct {
	ID                 int64     `json:"id"`
	SKU                string    `json:"sku"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	PriceCents         int64     `json:"price_cents"`
	Active             bool      `json:"active"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	UpdatedAtUnixMilli int64     `json:"updated_at_unix_milli"`
}

func newProductCacheRecord(product *domain.Product) productCacheRecord {
	return productCacheRecord{
		ID: product.ID(), SKU: product.SKU(), Name: product.Name(), Description: product.Description(),
		PriceCents: product.PriceCents(), Active: product.Active(),
		CreatedAt: product.CreatedAt(), UpdatedAt: product.UpdatedAt(),
		UpdatedAtUnixMilli: product.UpdatedAt().UnixMilli(),
	}
}

func (r productCacheRecord) product() (*domain.Product, error) {
	return domain.RehydrateProduct(domain.ProductSnapshot{
		ID: r.ID, SKU: r.SKU, Name: r.Name, Description: r.Description,
		PriceCents: r.PriceCents, Active: r.Active, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	})
}

func (c *RedisProductCache) Get(ctx context.Context, id int64) (*domain.Product, bool, error) {
	value, err := c.client.Do(ctx, c.client.B().Get().Key(productCacheKey(id)).Build()).ToString()
	if rueidis.IsRedisNil(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get product: %w", err)
	}
	var record productCacheRecord
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		_ = c.Delete(ctx, id)
		return nil, false, fmt.Errorf("decode cached product: %w", err)
	}
	product, err := record.product()
	if err != nil {
		_ = c.Delete(ctx, id)
		return nil, false, fmt.Errorf("rehydrate cached product: %w", err)
	}
	return product, true, nil
}

func (c *RedisProductCache) SetIfNewer(ctx context.Context, product *domain.Product) error {
	record := newProductCacheRecord(product)
	value, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode cached product: %w", err)
	}
	const script = `
local current = redis.call('GET', KEYS[1])
if current then
  local decoded = cjson.decode(current)
  if decoded.updated_at_unix_milli > tonumber(ARGV[1]) then
    return 0
  end
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
return 1`
	command := c.client.B().Eval().Script(script).Numkeys(1).Key(productCacheKey(product.ID())).Arg(
		strconv.FormatInt(record.UpdatedAtUnixMilli, 10), string(value), strconv.FormatInt(c.ttl.Milliseconds(), 10),
	).Build()
	if err := c.client.Do(ctx, command).Error(); err != nil {
		return fmt.Errorf("redis set product: %w", err)
	}
	return nil
}

func (c *RedisProductCache) Delete(ctx context.Context, id int64) error {
	if err := c.client.Do(ctx, c.client.B().Del().Key(productCacheKey(id)).Build()).Error(); err != nil {
		return fmt.Errorf("redis delete product: %w", err)
	}
	return nil
}
