package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
)

type fakeRepository struct {
	file         *domain.File
	markReadyErr error
	deleteErr    error
	ops          []string
}

func (r *fakeRepository) CreatePending(_ context.Context, file *domain.File) (*domain.File, error) {
	r.file = file
	r.ops = append(r.ops, "create-pending")
	return file, nil
}
func (r *fakeRepository) MarkReady(context.Context, string) (*domain.File, error) {
	r.ops = append(r.ops, "mark-ready")
	if r.markReadyErr != nil {
		return nil, r.markReadyErr
	}
	return fileInState(r.file, domain.StateReady), nil
}
func (r *fakeRepository) GetReadyOwned(context.Context, string, string) (*domain.File, error) {
	return r.file, nil
}
func (r *fakeRepository) ListReady(context.Context, domain.ListQuery) ([]*domain.File, int64, error) {
	return nil, 0, nil
}
func (r *fakeRepository) MarkDeleting(context.Context, string, string) (*domain.File, error) {
	r.ops = append(r.ops, "mark-deleting")
	return fileInState(r.file, domain.StateDeleting), nil
}
func (r *fakeRepository) DeleteMetadata(context.Context, string) error {
	r.ops = append(r.ops, "delete-metadata")
	return r.deleteErr
}
func (r *fakeRepository) ListStale(context.Context, time.Time, int) ([]*domain.File, error) {
	return nil, nil
}

type fakeBlobStore struct {
	putErr    error
	deleteErr error
	ops       *[]string
}

func (b *fakeBlobStore) Put(context.Context, string, []byte) error {
	*b.ops = append(*b.ops, "put-blob")
	return b.putErr
}
func (b *fakeBlobStore) Read(context.Context, string) ([]byte, error) { return nil, nil }
func (b *fakeBlobStore) Delete(context.Context, string) error {
	*b.ops = append(*b.ops, "delete-blob")
	return b.deleteErr
}

func fileInState(file *domain.File, state domain.State) *domain.File {
	value, err := domain.RehydrateFile(domain.FileSnapshot{
		ID: file.ID(), OwnerSubject: file.OwnerSubject(), Name: file.Name(), StorageKey: file.StorageKey(),
		ContentType: file.ContentType(), Size: file.Size(), SHA256: file.SHA256(), State: state,
	})
	if err != nil {
		panic(err)
	}
	return value
}

func TestUploadCompensatesMetadataWhenBlobWriteFails(t *testing.T) {
	repo := &fakeRepository{}
	blobs := &fakeBlobStore{putErr: errors.New("object store unavailable"), ops: &repo.ops}
	_, err := NewUsecase(repo, blobs, 1024).Upload(context.Background(), "owner", "a.txt", "text/plain", []byte("hello"))
	if err == nil {
		t.Fatal("Upload should fail")
	}
	want := []string{"create-pending", "put-blob", "delete-metadata"}
	if !reflect.DeepEqual(repo.ops, want) {
		t.Fatalf("operations = %v, want %v", repo.ops, want)
	}
}

func TestUploadLeavesPendingForCleanupWhenMarkReadyFails(t *testing.T) {
	repo := &fakeRepository{markReadyErr: errors.New("database unavailable")}
	blobs := &fakeBlobStore{ops: &repo.ops}
	_, err := NewUsecase(repo, blobs, 1024).Upload(context.Background(), "owner", "a.txt", "text/plain", []byte("hello"))
	if err == nil {
		t.Fatal("Upload should fail")
	}
	want := []string{"create-pending", "put-blob", "mark-ready"}
	if !reflect.DeepEqual(repo.ops, want) {
		t.Fatalf("operations = %v, want %v", repo.ops, want)
	}
}

func TestDeleteMarksMetadataBeforeDeletingBlob(t *testing.T) {
	file, err := domain.NewFile("id", "owner", "a.txt", "text/plain", 5, "hash")
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepository{file: file}
	blobs := &fakeBlobStore{ops: &repo.ops}
	if err := NewUsecase(repo, blobs, 1024).Delete(context.Background(), "owner", "id"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	want := []string{"mark-deleting", "delete-blob", "delete-metadata"}
	if !reflect.DeepEqual(repo.ops, want) {
		t.Fatalf("operations = %v, want %v", repo.ops, want)
	}
}
