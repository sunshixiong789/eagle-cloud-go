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

func TestUploadLeavesPendingWhenBlobWriteFails(t *testing.T) {
	repo := &fakeRepository{}
	blobs := &fakeBlobStore{putErr: errors.New("object store unavailable"), ops: &repo.ops}
	_, err := NewUsecase(repo, blobs, 1024).Upload(context.Background(), "owner", "a.txt", "text/plain", []byte("hello"))
	if err == nil {
		t.Fatal("Upload should fail")
	}
	want := []string{"create-pending", "put-blob"}
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

type pendingRepository struct{ fakeRepository }

func (r *pendingRepository) DeleteMetadata(ctx context.Context, id string) error {
	if err := r.fakeRepository.DeleteMetadata(ctx, id); err != nil {
		return err
	}
	r.file = nil
	return nil
}
func (r *pendingRepository) ListStale(context.Context, time.Time, int) ([]*domain.File, error) {
	if r.file == nil {
		return nil, nil
	}
	return []*domain.File{r.file}, nil
}

type lostResponseBlobStore struct {
	exists    bool
	deleteErr error
}

func (b *lostResponseBlobStore) Put(context.Context, string, []byte) error {
	b.exists = true
	return errors.New("response lost after object committed")
}
func (b *lostResponseBlobStore) Read(context.Context, string) ([]byte, error) { return nil, nil }
func (b *lostResponseBlobStore) Delete(context.Context, string) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	b.exists = false
	return nil
}

func TestUploadLostResponseCanBeCleanedAfterStorageRecovers(t *testing.T) {
	repo := &pendingRepository{}
	blobs := &lostResponseBlobStore{deleteErr: errors.New("storage unavailable")}
	uc := NewUsecase(repo, blobs, 1024)
	if _, err := uc.Upload(context.Background(), "owner", "a.txt", "text/plain", []byte("hello")); err == nil {
		t.Fatal("expected lost response")
	}
	if repo.file == nil || !blobs.exists {
		t.Fatal("uncertain upload must retain pending metadata")
	}
	if _, err := uc.CleanupStale(context.Background(), time.Now(), 100); err == nil {
		t.Fatal("expected cleanup failure")
	}
	if repo.file == nil {
		t.Fatal("failed blob deletion lost pending metadata")
	}
	blobs.deleteErr = nil
	cleaned, err := uc.CleanupStale(context.Background(), time.Now(), 100)
	if err != nil || cleaned != 1 || repo.file != nil || blobs.exists {
		t.Fatalf("cleanup=%d err=%v file=%v blob=%v", cleaned, err, repo.file, blobs.exists)
	}
}
