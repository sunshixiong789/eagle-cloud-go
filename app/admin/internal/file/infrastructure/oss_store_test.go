package infrastructure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

func TestOSSBlobStoreRoundTrip(t *testing.T) {
	var mu sync.Mutex
	objects := make(map[string][]byte)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "OSS4-HMAC-SHA256 ") || r.Header.Get("x-oss-security-token") != "test-token" {
			t.Error("request is missing OSS V4 authentication or STS token")
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/test-bucket/missing-bucket" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<Error><Code>NoSuchBucket</Code></Error>")
			return
		}
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			objects[r.URL.Path] = body
			w.Header().Set("ETag", `"test-etag"`)
		case http.MethodGet:
			body, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, "<Error><Code>NoSuchKey</Code></Error>")
				return
			}
			_, _ = w.Write(body)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()
	store, err := NewBlobStore(&config.File{Provider: "oss", Oss: &config.File_OSS{
		Endpoint: srv.URL, Region: "cn-hangzhou", Bucket: "test-bucket",
		AccessKeyId: "test-id", AccessKeySecret: "test-secret", SecurityToken: "test-token",
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Put(ctx, "file-id", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	content, err := store.Read(ctx, "file-id")
	if err != nil || string(content) != "hello" {
		t.Fatalf("read = %q, %v", content, err)
	}
	for range 2 {
		if err := store.Delete(ctx, "file-id"); err != nil {
			t.Fatalf("idempotent delete: %v", err)
		}
	}
	if _, err := store.Read(ctx, "file-id"); !errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("missing object = %v", err)
	}
	if _, err := store.Read(ctx, "missing-bucket"); err == nil || errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("bucket configuration error was treated as a missing file: %v", err)
	}
}

func TestOSSOutageDoesNotGateConstructionAndRecovers(t *testing.T) {
	var available atomic.Bool
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if !available.Load() {
			<-r.Context().Done()
			return
		}
		w.Header().Set("ETag", `"test-etag"`)
	}))
	defer srv.Close()
	store := NewOSSBlobStore(&config.File_OSS{Endpoint: srv.URL, Region: "cn-hangzhou", Bucket: "test-bucket", AccessKeyId: "test-id", AccessKeySecret: "test-secret"})
	if requests.Load() != 0 {
		t.Fatal("constructor contacted OSS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := store.Put(ctx, "file-id", []byte("hello")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("operation did not honor the caller deadline: %v", err)
	}
	available.Store(true)
	if err := store.Put(context.Background(), "file-id", []byte("hello")); err != nil {
		t.Fatalf("OSS failed to recover: %v", err)
	}
}
