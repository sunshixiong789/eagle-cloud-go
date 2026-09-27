package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
)

func TestLocalBlobStoreLifecycle(t *testing.T) {
	store, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const key = "12345678-1234-4234-9234-123456789abc"
	if err := store.Put(ctx, key, []byte("content")); err != nil {
		t.Fatal(err)
	}
	got, err := store.Read(ctx, key)
	if err != nil || string(got) != "content" {
		t.Fatalf("read = %q, %v", got, err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, key); !errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("read deleted blob = %v", err)
	}
}

func TestLocalBlobStoreRejectsTraversal(t *testing.T) {
	store, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "../escape", []byte("x")); err == nil {
		t.Fatal("path traversal key should be rejected")
	}
}
