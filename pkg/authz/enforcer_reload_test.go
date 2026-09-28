package authz

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/casbin/casbin/v2/model"
)

type versionedTestSource struct{ version atomic.Int64 }

func (s *versionedTestSource) PolicyVersion(context.Context) (int64, error) {
	return s.version.Load(), nil
}
func (s *versionedTestSource) LoadPolicyRows(context.Context) ([]StoredPolicy, error) {
	if s.version.Load() == 1 {
		return []StoredPolicy{{PType: "p", Values: []string{"realm:editor", "system:dict:remove"}}}, nil
	}
	return nil, nil
}

type pausedSnapshotAdapter struct {
	*StorageAdapter
	paused, resume chan struct{}
}

func (a *pausedSnapshotAdapter) LoadPolicySnapshot(ctx context.Context, m model.Model) (int64, error) {
	version, err := a.StorageAdapter.LoadPolicySnapshot(ctx, m)
	if err == nil && version == 1 {
		close(a.paused)
		<-a.resume
	}
	return version, err
}

func TestConcurrentReloadCannotRestoreRevokedPermission(t *testing.T) {
	source := &versionedTestSource{}
	adapter := &pausedSnapshotAdapter{StorageAdapter: NewStorageAdapter(source), paused: make(chan struct{}), resume: make(chan struct{})}
	enforcer, err := NewEnforcer(adapter)
	if err != nil {
		t.Fatal(err)
	}
	source.version.Store(1)
	done := make(chan error, 1)
	go func() { done <- enforcer.ReloadPolicy(context.Background()) }()
	<-adapter.paused
	// A slow database load must not block decisions on the current snapshot.
	if _, err := enforcer.Allow([]string{"realm:editor"}, "system:dict:remove"); err != nil {
		t.Fatal(err)
	}
	source.version.Store(2)
	err = enforcer.ReloadPolicy(context.Background())
	close(adapter.resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrPolicyVersionRegressed) {
		t.Fatalf("old reload: %v", err)
	}
	allowed, err := enforcer.Allow([]string{"realm:editor"}, "system:dict:remove")
	if err != nil || allowed || enforcer.LoadedPolicyVersion() != 2 {
		t.Fatalf("allowed=%v version=%d error=%v", allowed, enforcer.LoadedPolicyVersion(), err)
	}
}

func TestSnapshotVersionsAndFailedReloadPreservePolicy(t *testing.T) {
	ctx := context.Background()
	e, err := NewEnforcer(nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := []StoredPolicy{{PType: "p", Values: []string{"role", "system:dict:list"}}}
	// Version zero is a valid initial snapshot.
	if err := e.ReplacePolicySnapshot(ctx, rules, 0); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.Allow([]string{"role"}, "system:dict:list"); err != nil || !ok {
		t.Fatalf("initial snapshot: %v %v", ok, err)
	}
	if err := e.ReplacePolicySnapshot(ctx, nil, 2); err != nil {
		t.Fatal(err)
	}
	if err := e.ReplacePolicySnapshot(ctx, rules, 1); !errors.Is(err, ErrPolicyVersionRegressed) {
		t.Fatalf("regression: %v", err)
	}
	if ok, err := e.Allow([]string{"role"}, "system:dict:list"); err != nil || ok {
		t.Fatalf("revocation: %v %v", ok, err)
	}
	if err := e.ReplacePolicySnapshot(ctx, rules, 3); err != nil {
		t.Fatal(err)
	}
	e.adapter = NewStorageAdapter(failedPolicySource{})
	if err := e.ReloadPolicy(ctx); err == nil {
		t.Fatal("load should fail")
	}
	if ok, err := e.Allow([]string{"role"}, "system:dict:list"); err != nil || !ok || e.LoadedPolicyVersion() != 3 {
		t.Fatalf("last valid snapshot: %v %v version=%d", ok, err, e.LoadedPolicyVersion())
	}
}

type failedPolicySource struct{}

func (failedPolicySource) PolicyVersion(context.Context) (int64, error) { return 0, context.Canceled }
func (failedPolicySource) LoadPolicyRows(context.Context) ([]StoredPolicy, error) {
	return nil, context.Canceled
}
