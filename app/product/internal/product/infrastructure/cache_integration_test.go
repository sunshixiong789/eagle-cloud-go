package infrastructure

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

func redisTestConfig(address string) *config.Cache_Redis {
	return &config.Cache_Redis{Enabled: true, Address: address, Ttl: durationpb.New(time.Minute), OperationTimeout: durationpb.New(300 * time.Millisecond)}
}

func connectedCache(t *testing.T, c *config.Cache_Redis) *RedisProductCache {
	t.Helper()
	cache, closeCache, err := NewRedisProductCache(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeCache)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := cache.connection(); err == nil {
			return cache
		}
		if time.Now().After(deadline) {
			t.Fatal("Redis did not connect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRedisFillCannotResurrectDeletedOrUpdatedProduct(t *testing.T) {
	address := os.Getenv("EAGLE_TEST_REDIS_ADDRESS")
	if address == "" {
		t.Skip("set EAGLE_TEST_REDIS_ADDRESS to run real Redis tests")
	}
	cache := connectedCache(t, redisTestConfig(address))
	ctx := context.Background()
	id := time.Now().UnixNano()
	t.Cleanup(func() { _ = cache.Invalidate(ctx, id) })
	value, err := domain.RehydrateProduct(domain.ProductSnapshot{ID: id, SKU: "test", Name: "old", PriceCents: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, oldToken, err := cache.Lookup(ctx, id)
	if err != nil || oldToken == "" {
		t.Fatalf("miss token: %q %v", oldToken, err)
	}
	// A delete/update commits and invalidates while the original DB read is delayed.
	if err := cache.Invalidate(ctx, id); err != nil {
		t.Fatal(err)
	}
	_, newToken, err := cache.Lookup(ctx, id)
	if err != nil || newToken == oldToken {
		t.Fatalf("new generation: %q %v", newToken, err)
	}
	if err := cache.Fill(ctx, value, oldToken); err != nil {
		t.Fatal(err)
	}
	if got, _, err := cache.Lookup(ctx, id); err != nil || got != nil {
		t.Fatalf("stale read resurrected: %v %v", got, err)
	}
	if err := value.Update(domain.UpdateProductParams{Name: "new", PriceCents: 2}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Fill(ctx, value, newToken); err != nil {
		t.Fatal(err)
	}
	if got, _, err := cache.Lookup(ctx, id); err != nil || got == nil || got.Name() != "new" {
		t.Fatalf("new fill lost: %v %v", got, err)
	}
	// Eviction/expiry also removes ownership: a late reader must not recreate it.
	if err := cache.Invalidate(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := cache.Fill(ctx, value, newToken); err != nil {
		t.Fatal(err)
	}
	if got, _, err := cache.Lookup(ctx, id); err != nil || got != nil {
		t.Fatalf("expired token recreated: %v %v", got, err)
	}
}

func TestRedisUnavailableDoesNotPreventStartupOrDatabaseFallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	cache, closeCache, err := NewRedisProductCache(redisTestConfig(address))
	if err != nil {
		t.Fatal(err)
	}
	defer closeCache()
	value, err := domain.RehydrateProduct(domain.ProductSnapshot{ID: 7, SKU: "test", Name: "DB", PriceCents: 1})
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeProductRepository{value: value}
	if got, err := NewCachedRepository(repo, cache, nil).Get(context.Background(), 7); err != nil || got != value {
		t.Fatalf("fallback: %v %v", got, err)
	}
	disabled, closeDisabled, err := NewRedisProductCache(&config.Cache_Redis{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeDisabled()
	if got, token, err := disabled.Lookup(context.Background(), 7); err != nil || got != nil || token != "" {
		t.Fatalf("disabled: %v %s %v", got, token, err)
	}
}

func TestRedisTLSVerifiesCAAndServerName(t *testing.T) {
	address := os.Getenv("EAGLE_TEST_REDIS_ADDRESS")
	if address == "" {
		t.Skip("set EAGLE_TEST_REDIS_ADDRESS to run real Redis TLS tests")
	}
	tlsAddress, ca := redisTLSProxy(t, address)
	c := redisTestConfig(tlsAddress)
	c.TlsEnabled = true
	c.TlsCaFile = ca
	c.TlsServerName = "localhost"
	cache := connectedCache(t, c)
	id := time.Now().UnixNano()
	if _, _, err := cache.Lookup(context.Background(), id); err != nil {
		t.Fatalf("verified TLS Redis request: %v", err)
	}
	t.Cleanup(func() { _ = cache.Invalidate(context.Background(), id) })
	for _, bad := range []*config.Cache_Redis{
		{TlsEnabled: true, TlsServerName: "localhost"},
		{TlsEnabled: true, TlsCaFile: ca, TlsServerName: "wrong.example"},
	} {
		tc, err := redisTLSConfig(bad)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", tlsAddress, tc)
		if err == nil {
			_ = conn.Close()
			t.Fatal("TLS accepted an untrusted CA or wrong server name")
		}
	}
}

func redisTLSProxy(t *testing.T, upstream string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var connections []net.Conn
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.DialTimeout("tcp", upstream, time.Second)
			if err != nil {
				_ = client.Close()
				continue
			}
			mu.Lock()
			connections = append(connections, client, server)
			mu.Unlock()
			go func() { _, _ = io.Copy(server, client); _ = server.Close() }()
			go func() { _, _ = io.Copy(client, server); _ = client.Close() }()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-stopped
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
	return listener.Addr().String(), ca
}
