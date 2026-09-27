package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
)

type LocalBlobStore struct{ root string }

func NewLocalBlobStore(root string) (*LocalBlobStore, error) {
	root = filepath.Clean(root)
	if root == "." || root == "" {
		return nil, errors.New("file storage directory is required")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create file storage directory: %w", err)
	}
	return &LocalBlobStore{root: root}, nil
}

func (s *LocalBlobStore) path(key string) (string, error) {
	if key == "" || filepath.Base(key) != key || strings.ContainsAny(key, `/\\`) {
		return "", errors.New("invalid storage key")
	}
	prefix := key
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return filepath.Join(s.root, prefix, key), nil
}

func (s *LocalBlobStore) Put(_ context.Context, key string, content []byte) error {
	target, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return fmt.Errorf("create temporary blob: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close blob: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("commit blob: %w", err)
	}
	return nil
}

func (s *LocalBlobStore) Read(_ context.Context, key string) ([]byte, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- path() rejects separators and traversal before joining root.
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}
	return content, nil
}

func (s *LocalBlobStore) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}
