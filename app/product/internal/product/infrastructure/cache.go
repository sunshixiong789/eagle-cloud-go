package infrastructure

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/redis/rueidis"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type productCache interface {
	Lookup(context.Context, int64) (*domain.Product, string, error)
	Fill(context.Context, *domain.Product, string) error
	Invalidate(context.Context, int64) error
}

// RedisProductCache uses a random fill token, not wall-clock timestamps. A write
// invalidates the token so a DB read that began before that write cannot refill it.
type RedisProductCache struct {
	mu      sync.RWMutex
	client  rueidis.Client
	ttl     time.Duration
	timeout time.Duration
	enabled bool
}

func NewRedisProductCache(c *config.Cache_Redis) (*RedisProductCache, func(), error) {
	cache := &RedisProductCache{ttl: c.GetTtl().AsDuration(), timeout: c.GetOperationTimeout().AsDuration(), enabled: c.GetEnabled()}
	if !cache.enabled {
		return cache, func() {}, nil
	}
	tlsConfig, err := redisTLSConfig(c)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	// Cache availability must not gate process startup. rueidis reconnects once a
	// client exists; this loop also recovers from an initial connection failure.
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			client, err := rueidis.NewClient(rueidis.ClientOption{
				InitAddress: []string{c.GetAddress()}, Username: c.GetUsername(), Password: c.GetPassword(),
				SelectDB: int(c.GetDatabase()), ClientName: "eagle-product", TLSConfig: tlsConfig,
				Dialer: net.Dialer{Timeout: cache.timeout}, ConnWriteTimeout: cache.timeout,
				DisableCache: true,
				DialCtxFn: func(_ context.Context, address string, dialer *net.Dialer, tc *tls.Config) (net.Conn, error) {
					attempt, stop := context.WithTimeout(ctx, cache.timeout)
					defer stop()
					if tc != nil {
						return (&tls.Dialer{NetDialer: dialer, Config: tc}).DialContext(attempt, "tcp", address)
					}
					return dialer.DialContext(attempt, "tcp", address)
				},
			})
			if err == nil {
				cache.mu.Lock()
				cache.client = client
				cache.mu.Unlock()
				<-ctx.Done()
				client.Close()
				return
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return cache, func() { cancel(); <-done }, nil
}

func redisTLSConfig(c *config.Cache_Redis) (*tls.Config, error) {
	if !c.GetTlsEnabled() {
		return nil, nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.GetTlsServerName()}
	if c.GetTlsCaFile() != "" {
		pem, err := os.ReadFile(c.GetTlsCaFile()) // #nosec G304,G703 -- CA path is trusted operator configuration, never request input.
		if err != nil {
			return nil, fmt.Errorf("read Redis CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("redis CA file contains no certificates")
		}
		tc.RootCAs = roots
	}
	return tc, nil
}

func productCacheKey(id int64) string { return "eagle:product:v3:" + strconv.FormatInt(id, 10) }

func (c *RedisProductCache) connection() (rueidis.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.client == nil {
		return nil, errors.New("redis cache unavailable")
	}
	return c.client, nil
}

func (c *RedisProductCache) Lookup(ctx context.Context, id int64) (*domain.Product, string, error) {
	if !c.enabled {
		return nil, "", nil
	}
	client, err := c.connection()
	if err != nil {
		return nil, "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	const script = `
local value = redis.call('HGET', KEYS[1], 'value')
if value then return {value, ''} end
local token = redis.call('HGET', KEYS[1], 'token')
if not token then
 token = ARGV[1]
 redis.call('HSET', KEYS[1], 'token', token)
 redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return {'', token}`
	values, err := client.Do(ctx, client.B().Eval().Script(script).Numkeys(1).Key(productCacheKey(id)).Arg(hex.EncodeToString(random[:]), strconv.FormatInt(c.ttl.Milliseconds(), 10)).Build()).AsStrSlice()
	if err != nil {
		return nil, "", fmt.Errorf("lookup Redis product: %w", err)
	}
	if len(values) != 2 {
		return nil, "", errors.New("invalid Redis product response")
	}
	if values[0] == "" {
		return nil, values[1], nil
	}
	var snapshot domain.ProductSnapshot
	if err := json.Unmarshal([]byte(values[0]), &snapshot); err != nil {
		_ = c.Invalidate(ctx, id)
		return nil, "", err
	}
	product, err := domain.RehydrateProduct(snapshot)
	if err != nil {
		_ = c.Invalidate(ctx, id)
	}
	return product, "", err
}

func (c *RedisProductCache) Fill(ctx context.Context, product *domain.Product, token string) error {
	if !c.enabled || token == "" {
		return nil
	}
	client, err := c.connection()
	if err != nil {
		return err
	}
	value, err := json.Marshal(domain.ProductSnapshot{
		ID: product.ID(), SKU: product.SKU(), Name: product.Name(), Description: product.Description(),
		PriceCents: product.PriceCents(), Active: product.Active(), CreatedAt: product.CreatedAt(), UpdatedAt: product.UpdatedAt(),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// An expired or invalidated token must never be recreated by a delayed reader.
	const script = `
if redis.call('HGET', KEYS[1], 'token') ~= ARGV[1] then return 0 end
redis.call('HSET', KEYS[1], 'value', ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1`
	return client.Do(ctx, client.B().Eval().Script(script).Numkeys(1).Key(productCacheKey(product.ID())).Arg(token, string(value), strconv.FormatInt(c.ttl.Milliseconds(), 10)).Build()).Error()
}

func (c *RedisProductCache) Invalidate(ctx context.Context, id int64) error {
	if !c.enabled {
		return nil
	}
	client, err := c.connection()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return client.Do(ctx, client.B().Del().Key(productCacheKey(id)).Build()).Error()
}
