// Package domain contains the file module's metadata lifecycle and storage ports.
package domain

import (
	"context"
	"errors"
	"mime"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrFileNotFound       = errors.New("file: 文件不存在")
	ErrFileTooLarge       = errors.New("file: 文件超过大小限制")
	ErrInvalidFileName    = errors.New("file: 文件名不合法")
	ErrInvalidContentType = errors.New("file: 内容类型不合法")
	ErrInvalidFileState   = errors.New("file: 文件状态不合法")
)

type State string

const (
	StatePending  State = "pending"
	StateReady    State = "ready"
	StateDeleting State = "deleting"
)

func (s State) Valid() bool { return s == StatePending || s == StateReady || s == StateDeleting }

// File 是对象存储与元数据之间的生命周期聚合。
type File struct {
	id           string
	ownerSubject string
	name         string
	storageKey   string
	contentType  string
	size         int64
	sha256       string
	state        State
	createdAt    time.Time
	updatedAt    time.Time
}

func NewFile(id, owner, name, contentType string, size int64, sha256 string) (*File, error) {
	name = strings.TrimSpace(name)
	if id == "" || owner == "" || size < 0 || sha256 == "" || name == "" || name == "." || name == ".." ||
		utf8.RuneCountInString(name) > 255 || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return nil, ErrInvalidFileName
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if len(contentType) > 128 {
		return nil, ErrInvalidContentType
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, ErrInvalidContentType
	}
	contentType = mime.FormatMediaType(mediaType, params)
	if contentType == "" {
		return nil, ErrInvalidContentType
	}
	return &File{
		id: id, ownerSubject: owner, name: name, storageKey: id,
		contentType: contentType, size: size, sha256: sha256, state: StatePending,
	}, nil
}

type FileSnapshot struct {
	ID           string
	OwnerSubject string
	Name         string
	StorageKey   string
	ContentType  string
	Size         int64
	SHA256       string
	State        State
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func RehydrateFile(snapshot FileSnapshot) (*File, error) {
	if !snapshot.State.Valid() {
		return nil, ErrInvalidFileState
	}
	file, err := NewFile(snapshot.ID, snapshot.OwnerSubject, snapshot.Name, snapshot.ContentType, snapshot.Size, snapshot.SHA256)
	if err != nil {
		return nil, err
	}
	file.storageKey = snapshot.StorageKey
	file.state = snapshot.State
	file.createdAt = snapshot.CreatedAt
	file.updatedAt = snapshot.UpdatedAt
	return file, nil
}

func (f *File) ID() string           { return f.id }
func (f *File) OwnerSubject() string { return f.ownerSubject }
func (f *File) Name() string         { return f.name }
func (f *File) StorageKey() string   { return f.storageKey }
func (f *File) ContentType() string  { return f.contentType }
func (f *File) Size() int64          { return f.size }
func (f *File) SHA256() string       { return f.sha256 }
func (f *File) State() State         { return f.state }
func (f *File) CreatedAt() time.Time { return f.createdAt }
func (f *File) UpdatedAt() time.Time { return f.updatedAt }

type ListQuery struct {
	OwnerSubject string
	Offset       int64
	PageSize     int32
}

type Repository interface {
	CreatePending(context.Context, *File) (*File, error)
	MarkReady(context.Context, string) (*File, error)
	GetReadyOwned(context.Context, string, string) (*File, error)
	ListReady(context.Context, ListQuery) ([]*File, int64, error)
	MarkDeleting(context.Context, string, string) (*File, error)
	DeleteMetadata(context.Context, string) error
	ListStale(context.Context, time.Time, int) ([]*File, error)
}

type BlobStore interface {
	Put(context.Context, string, []byte) error
	Read(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
