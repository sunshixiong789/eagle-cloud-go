package e2e

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
	"github.com/eagle-go/eagle/tests/testkit"
)

// Runs the real composition roots in separate processes, with real PostgreSQL
// databases and RabbitMQ. Only Keycloak token issuance is replaced; all services
// validate RS256 signatures, audiences, and service identities normally.
func TestOrderEventAcrossServiceProcesses(t *testing.T) {
	broker := os.Getenv("EAGLE_TEST_RABBITMQ_URL")
	if testing.Short() || broker == "" {
		t.Skip("requires EAGLE_TEST_RABBITMQ_URL pointing to an isolated test broker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	root, err := testkit.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	pg, err := testkit.StartPostgres("cross-service", "eagle_admin_e2e", "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() })
	productDSN, err := pg.CreateDatabase("eagle_product_e2e", "product")
	if err != nil {
		t.Fatal(err)
	}
	orderDSN, err := pg.CreateDatabase("eagle_order_e2e", "order")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", orderDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	identityServer, token, issued := newIdentityServer(t)
	userToken := token("eagle-web", "owner", []string{"eagle-admin", "eagle-product", "eagle-order"}, false)
	conn, err := amqp.Dial(broker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	exchange := fmt.Sprintf("eagle.e2e.%d", time.Now().UnixNano())
	queue := exchange + ".orders"
	t.Cleanup(func() {
		_, _ = ch.QueueDelete(queue, false, false, false)
		_, _ = ch.QueueDelete(queue+".dlq", false, false, false)
		_ = ch.ExchangeDelete(exchange, false, false)
		_ = ch.ExchangeDelete(exchange+".dlx", false, false)
	})
	services := make(map[string]*serviceProcess)
	for _, name := range []string{"admin", "product", "order"} {
		binary := filepath.Join(t.TempDir(), name)
		build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./app/"+name+"/cmd/"+name)
		build.Dir = root
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, output)
		}
		services[name] = &serviceProcess{name: name, binary: binary, root: root, http: freeAddress(t), grpc: freeAddress(t), metrics: freeAddress(t)}
	}
	for _, name := range []string{"admin", "product", "order"} {
		service := services[name]
		dsn := map[string]string{"admin": pg.DSN, "product": productDSN, "order": orderDSN}[name]
		service.env = []string{
			"EAGLE_DATABASE_DSN=" + dsn, "EAGLE_SERVER_HTTP_ADDR=" + service.http, "EAGLE_SERVER_GRPC_ADDR=" + service.grpc, "EAGLE_OBSERVABILITY_METRICS_ADDR=" + service.metrics,
			"EAGLE_OBSERVABILITY_OTLP_ENDPOINT=", "EAGLE_AUTH_ISSUER=" + identityServer.URL + "/realms/eagle", "EAGLE_AUTH_CLIENT_ID=eagle-" + name, "EAGLE_AUTH_AUDIENCE=eagle-" + name,
			"EAGLE_SERVICE_AUTH_TOKEN_URL=" + identityServer.URL + "/realms/eagle/protocol/openid-connect/token",
			"EAGLE_MESSAGING_RABBITMQ_URL=" + broker, "EAGLE_MESSAGING_EXCHANGE=" + exchange, "EAGLE_MESSAGING_ORDER_CREATED_QUEUE=" + queue,
			"EAGLE_CACHE_REDIS_ENABLED=false", "EAGLE_UPSTREAM_AUTHORIZATION_ENDPOINT=" + services["admin"].grpc, "EAGLE_UPSTREAM_PRODUCT_ENDPOINT=" + services["product"].grpc,
			// S3 is deliberately unreachable: policy and notification startup must work.
			"EAGLE_FILE_PROVIDER=s3", "EAGLE_FILE_S3_ENDPOINT=127.0.0.1:1", "EAGLE_FILE_S3_REGION=us-east-1",
		}
		switch name {
		case "admin":
			service.env = append(service.env, "EAGLE_AUTH_INTERNAL_CLIENT_ID=eagle-product-worker")
		case "product":
			service.env = append(service.env, "EAGLE_AUTH_INTERNAL_CLIENT_ID=eagle-order-worker", "EAGLE_SERVICE_AUTH_CLIENT_ID=eagle-product-worker", "EAGLE_SERVICE_AUTH_CLIENT_SECRET=test-product-secret")
		case "order":
			service.env = append(service.env, "EAGLE_SERVICE_AUTH_CLIENT_ID=eagle-order-worker", "EAGLE_SERVICE_AUTH_CLIENT_SECRET=test-order-secret")
		}
		service.start(t)
		t.Cleanup(func() { service.stop(t) })
		eventually(t, ctx, "ready "+name, func() bool {
			resp, err := (&http.Client{Timeout: time.Second}).Get("http://" + service.metrics + "/readyz")
			if err != nil {
				return false
			}
			defer func() { _ = resp.Body.Close() }()
			return resp.StatusCode == 200
		})
	}
	call := func(name, method, path, auth string, body any, want int) []byte {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, method, "http://"+services[name].http+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d: %s", method, path, resp.StatusCode, want, out)
		}
		return out
	}
	call("order", "GET", "/v1/orders", "", nil, 401)
	var product struct {
		Product struct {
			ID int64 `json:"id"`
		} `json:"product"`
	}
	if err := json.Unmarshal(call("product", "POST", "/v1/products", userToken, map[string]any{"sku": "e2e-sku", "name": "e2e product", "price_cents": 1250, "active": true}, 200), &product); err != nil {
		t.Fatal(err)
	}
	// Remove only this test binding. Orders must still commit, retaining outbox work.
	eventually(t, ctx, "consumer topology", func() bool {
		probe, err := conn.Channel()
		if err != nil {
			return false
		}
		defer func() { _ = probe.Close() }()
		q, err := probe.QueueDeclarePassive(queue, true, false, false, false, amqp.Table{"x-queue-type": "quorum"})
		return err == nil && q.Consumers > 0
	})
	if err := ch.QueueUnbind(queue, "order.created.v1", exchange, nil); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"idempotency_key": "same-order", "items": []any{map[string]any{"product_id": product.Product.ID, "quantity": 2}}}
	var created struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	first := call("order", "POST", "/v1/orders", userToken, request, 200)
	if err := json.Unmarshal(first, &created); err != nil || created.Order.ID == "" {
		t.Fatalf("order: %v %s", err, first)
	}
	var eventID string
	var payload []byte
	eventually(t, ctx, "unroutable outbox retained", func() bool {
		return db.QueryRowContext(ctx, "SELECT id,payload FROM event_outbox WHERE aggregate_id=$1 AND published_at IS NULL AND attempts>0", created.Order.ID).Scan(&eventID, &payload) == nil
	})
	if err := ch.QueueBind(queue, "order.created.v1", exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	notificationCount := func() int {
		var result struct {
			Notifications []json.RawMessage `json:"notifications"`
		}
		if err := json.Unmarshal(call("admin", "GET", "/v1/system/notifications", userToken, nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		return len(result.Notifications)
	}
	eventually(t, ctx, "outbox delivered", func() bool { return notificationCount() == 1 })
	// Restart the order process and retry the same client intent.
	services["order"].stop(t)
	services["order"].start(t)
	eventually(t, ctx, "order restarted", func() bool {
		resp, err := (&http.Client{Timeout: time.Second}).Get("http://" + services["order"].metrics + "/readyz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == 200
	})
	var retried struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(call("order", "POST", "/v1/orders", userToken, request, 200), &retried); err != nil || retried.Order.ID != created.Order.ID {
		t.Fatalf("retry created another order: %v %+v", err, retried)
	}
	// Replaying the exact event after consumer restart must hit the inbox barrier.
	services["admin"].stop(t)
	services["admin"].start(t)
	eventually(t, ctx, "admin restarted", func() bool {
		resp, err := (&http.Client{Timeout: time.Second}).Get("http://" + services["admin"].metrics + "/readyz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == 200
	})
	publisher, err := rabbitmq.NewPublisher(rabbitmq.Config{URL: broker, Exchange: exchange})
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	for range 3 {
		if err := publisher.Publish(ctx, rabbitmq.Message{ID: eventID, Type: "eagle.event.v1.OrderCreatedV1", RoutingKey: "order.created.v1", Body: payload}); err != nil {
			t.Fatal(err)
		}
	}
	// A new order is a processing barrier after those duplicate deliveries.
	request["idempotency_key"] = "next-order"
	call("order", "POST", "/v1/orders", userToken, request, 200)
	eventually(t, ctx, "duplicates consumed before second order", func() bool { return notificationCount() >= 2 })
	if count := notificationCount(); count != 2 {
		t.Fatalf("duplicate notification: %d", count)
	}
	if !issued("eagle-product-worker") || !issued("eagle-order-worker") {
		t.Fatal("service credential flows were not exercised")
	}
}

type serviceProcess struct {
	name, binary, root, http, grpc, metrics string
	env                                     []string
	cmd                                     *exec.Cmd
	done                                    chan error
	log                                     *os.File
}

func (s *serviceProcess) start(t *testing.T) {
	t.Helper()
	s.cmd = exec.Command(s.binary, "-conf", filepath.Join(s.root, "app", s.name, "configs"))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "EAGLE_") {
			s.cmd.Env = append(s.cmd.Env, entry)
		}
	}
	s.cmd.Env = append(s.cmd.Env, s.env...)
	var err error
	s.log, err = os.CreateTemp(t.TempDir(), s.name+"-*.log")
	if err != nil {
		t.Fatal(err)
	}
	s.cmd.Stdout = s.log
	s.cmd.Stderr = s.log
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s.done = make(chan error, 1)
	go func() { s.done <- s.cmd.Wait() }()
}
func (s *serviceProcess) stop(t *testing.T) {
	t.Helper()
	if s.cmd == nil {
		return
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-s.done:
		if err != nil {
			t.Errorf("%s exit: %v", s.name, err)
		}
	case <-time.After(15 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
		t.Errorf("%s shutdown timed out", s.name)
	}
	_ = s.log.Close()
	if t.Failed() {
		data, _ := os.ReadFile(s.log.Name())
		t.Logf("%s logs: %s", s.name, data)
	}
	s.cmd = nil
}
func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}
func eventually(t *testing.T, ctx context.Context, label string, check func() bool) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", label, ctx.Err())
		case <-deadline.C:
			t.Fatalf("timeout: %s", label)
		case <-ticker.C:
		}
	}
}

func newIdentityServer(t *testing.T) (*httptest.Server, func(string, string, []string, bool) string, func(string) bool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	var mu sync.Mutex
	issued := map[string]bool{}
	mint := func(client, subject string, aud []string, service bool) string {
		username := subject
		if service {
			username = "service-account-" + client
		}
		claims := map[string]any{"iss": issuer, "sub": subject, "aud": aud, "azp": client, "preferred_username": username, "exp": time.Now().Add(10 * time.Minute).Unix(), "resource_access": map[string]any{"eagle-product": map[string]any{"roles": []string{"admin"}}}}
		header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
		body, _ := json.Marshal(claims)
		encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
		hash := sha256.Sum256([]byte(encoded))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if err != nil {
			panic(err)
		}
		return encoded + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/eagle/protocol/openid-connect/certs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "test-key", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	})
	mux.HandleFunc("/realms/eagle/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		client := r.Form.Get("client_id")
		secrets := map[string]string{"eagle-product-worker": "test-product-secret", "eagle-order-worker": "test-order-secret"}
		if secrets[client] == "" || r.Form.Get("client_secret") != secrets[client] || r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "invalid client", http.StatusUnauthorized)
			return
		}
		aud := map[string]string{"eagle-product-worker": "eagle-admin", "eagle-order-worker": "eagle-product"}[client]
		mu.Lock()
		issued[client] = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": mint(client, "sub-"+client, []string{aud}, true), "expires_in": 600, "token_type": "Bearer"})
	})
	srv := httptest.NewServer(mux)
	issuer = srv.URL + "/realms/eagle"
	t.Cleanup(srv.Close)
	return srv, mint, func(client string) bool { mu.Lock(); defer mu.Unlock(); return issued[client] }
}
