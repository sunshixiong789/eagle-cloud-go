package infrastructure

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eagle-go/eagle/pkg/platform/config"
)

func TestS3OutageDoesNotGateConstructionAndRecovers(t *testing.T) {
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
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	store, err := NewS3BlobStore(&config.File_S3{Endpoint: strings.TrimPrefix(srv.URL, "http://"), Region: "us-east-1", Bucket: "test-bucket", AccessKey: "test", SecretKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("constructor contacted object storage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := store.Put(ctx, "file-id", []byte("hello")); err == nil {
		t.Fatal("unavailable store should fail file request")
	}
	available.Store(true)
	if err := store.Put(context.Background(), "file-id", []byte("hello")); err != nil {
		t.Fatalf("store failed to recover: %v", err)
	}
}
