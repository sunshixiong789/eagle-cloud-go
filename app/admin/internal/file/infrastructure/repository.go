// Package infrastructure implements file metadata persistence.
package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/file/domain"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/file"
)

type repository struct{ db *platformdb.Database }

func NewRepository(db *platformdb.Database) domain.Repository { return &repository{db: db} }

func toDomain(row *ent.File) (*domain.File, error) {
	if row == nil {
		return nil, nil
	}
	value, err := domain.RehydrateFile(domain.FileSnapshot{
		ID: row.ID, OwnerSubject: row.OwnerSubject, Name: row.Name,
		StorageKey: row.StorageKey, ContentType: row.ContentType,
		Size: row.Size, SHA256: row.Sha256, State: domain.State(row.State),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("rehydrate file metadata %s: %w", row.ID, err)
	}
	return value, nil
}

func (r *repository) CreatePending(ctx context.Context, value *domain.File) (*domain.File, error) {
	row, err := r.db.Client().File.Create().
		SetID(value.ID()).
		SetOwnerSubject(value.OwnerSubject()).
		SetName(value.Name()).
		SetStorageKey(value.StorageKey()).
		SetContentType(value.ContentType()).
		SetSize(value.Size()).
		SetSha256(value.SHA256()).
		SetState(string(domain.StatePending)).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create pending file metadata: %w", err)
	}
	return toDomain(row)
}

func (r *repository) MarkReady(ctx context.Context, id string) (*domain.File, error) {
	row, err := r.db.Client().File.Query().Where(file.IDEQ(id), file.StateEQ(string(domain.StatePending))).Only(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrInvalidFileState
		}
		return nil, fmt.Errorf("get pending file metadata: %w", err)
	}
	row, err = row.Update().SetState(string(domain.StateReady)).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark file ready: %w", err)
	}
	return toDomain(row)
}

func (r *repository) GetReadyOwned(ctx context.Context, owner, id string) (*domain.File, error) {
	row, err := r.db.Client().File.Query().Where(
		file.IDEQ(id), file.OwnerSubjectEQ(owner), file.StateEQ(string(domain.StateReady)),
	).Only(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrFileNotFound
		}
		return nil, fmt.Errorf("get ready file metadata: %w", err)
	}
	return toDomain(row)
}

func (r *repository) ListReady(ctx context.Context, q domain.ListQuery) ([]*domain.File, int64, error) {
	query := r.db.Client().File.Query().Where(
		file.OwnerSubjectEQ(q.OwnerSubject), file.StateEQ(string(domain.StateReady)),
	)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count files: %w", err)
	}
	rows, err := query.Order(ent.Desc(file.FieldCreatedAt)).Offset(int(q.Offset)).Limit(int(q.PageSize)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list files: %w", err)
	}
	out := make([]*domain.File, 0, len(rows))
	for _, row := range rows {
		value, err := toDomain(row)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, value)
	}
	return out, int64(total), nil
}

func (r *repository) MarkDeleting(ctx context.Context, owner, id string) (*domain.File, error) {
	row, err := r.db.Client().File.Query().Where(
		file.IDEQ(id), file.OwnerSubjectEQ(owner),
		file.StateIn(string(domain.StateReady), string(domain.StateDeleting)),
	).Only(ctx)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrFileNotFound
		}
		return nil, fmt.Errorf("get file for deletion: %w", err)
	}
	if row.State != string(domain.StateDeleting) {
		row, err = row.Update().SetState(string(domain.StateDeleting)).Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("mark file deleting: %w", err)
		}
	}
	return toDomain(row)
}

func (r *repository) DeleteMetadata(ctx context.Context, id string) error {
	if _, err := r.db.Client().File.Delete().Where(file.IDEQ(id)).Exec(ctx); err != nil {
		return fmt.Errorf("delete file metadata: %w", err)
	}
	return nil
}

func (r *repository) ListStale(ctx context.Context, before time.Time, limit int) ([]*domain.File, error) {
	rows, err := r.db.Client().File.Query().Where(
		file.StateIn(string(domain.StatePending), string(domain.StateDeleting)),
		file.UpdatedAtLT(before),
	).Order(ent.Asc(file.FieldUpdatedAt)).Limit(limit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list stale file metadata: %w", err)
	}
	out := make([]*domain.File, 0, len(rows))
	for _, row := range rows {
		value, err := toDomain(row)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}
