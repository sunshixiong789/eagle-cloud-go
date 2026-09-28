package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

var (
	ErrAdapterReadOnly         = errors.New("authz: policy adapter is load-only")
	ErrPolicyChangedDuringLoad = errors.New("authz: policy changed repeatedly during load")
	ErrPolicyVersionRegressed  = errors.New("authz: policy version regressed")
)

type StoredPolicy struct {
	PType  string
	Values []string
}

type PolicySource interface {
	LoadPolicyRows(context.Context) ([]StoredPolicy, error)
	PolicyVersion(context.Context) (int64, error)
}

// StorageAdapter adapts a project-owned policy store to Casbin's read API.
// Writes stay in the owning service so its transaction and audit rules cannot
// be bypassed through Casbin AutoSave.
type StorageAdapter struct {
	source PolicySource
}

func NewStorageAdapter(source PolicySource) *StorageAdapter {
	return &StorageAdapter{source: source}
}

func (a *StorageAdapter) LoadPolicy(m model.Model) error {
	return a.LoadPolicyContext(context.Background(), m)
}

func (a *StorageAdapter) LoadPolicyContext(ctx context.Context, m model.Model) error {
	_, err := a.LoadPolicySnapshot(ctx, m)
	return err
}

// LoadPolicySnapshot binds the returned version to this model, even when loads overlap.
func (a *StorageAdapter) LoadPolicySnapshot(ctx context.Context, m model.Model) (int64, error) {
	var rows []StoredPolicy
	var version int64
	stable := false
	for range 5 {
		before, err := a.source.PolicyVersion(ctx)
		if err != nil {
			return 0, err
		}
		rows, err = a.source.LoadPolicyRows(ctx)
		if err != nil {
			return 0, err
		}
		after, err := a.source.PolicyVersion(ctx)
		if err != nil {
			return 0, err
		}
		if before == after {
			version = after
			stable = true
			break
		}
	}
	if !stable {
		return 0, ErrPolicyChangedDuringLoad
	}
	for _, row := range rows {
		line := append([]string{row.PType}, row.Values...)
		if err := persist.LoadPolicyArray(line, m); err != nil {
			return 0, fmt.Errorf("authz: load policy %v: %w", line, err)
		}
	}
	return version, nil
}

func (a *StorageAdapter) SavePolicy(model.Model) error {
	return fmt.Errorf("authz: SavePolicy: %w", ErrAdapterReadOnly)
}

func (a *StorageAdapter) AddPolicy(string, string, []string) error {
	return fmt.Errorf("authz: AddPolicy: %w", ErrAdapterReadOnly)
}

func (a *StorageAdapter) RemovePolicy(string, string, []string) error {
	return fmt.Errorf("authz: RemovePolicy: %w", ErrAdapterReadOnly)
}

func (a *StorageAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return fmt.Errorf("authz: RemoveFilteredPolicy: %w", ErrAdapterReadOnly)
}

var _ persist.Adapter = (*StorageAdapter)(nil)
