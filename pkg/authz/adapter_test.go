package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/casbin/casbin/v2/model"
)

func TestStorageAdapterRejectsCasbinWrites(t *testing.T) {
	a := NewStorageAdapter(nil)
	m, err := model.NewModelFromString(ModelText)
	if err != nil {
		t.Fatalf("model: %v", err)
	}

	writes := []struct {
		name string
		err  error
	}{
		{"SavePolicy", a.SavePolicy(m)},
		{"AddPolicy", a.AddPolicy("", "p", []string{"r", "system:dict:list"})},
		{"RemovePolicy", a.RemovePolicy("", "p", []string{"r", "system:dict:list"})},
		{"RemoveFilteredPolicy", a.RemoveFilteredPolicy("", "p", 0, "r")},
	}
	for _, tc := range writes {
		if !errors.Is(tc.err, ErrAdapterReadOnly) {
			t.Errorf("%s = %v, want ErrAdapterReadOnly", tc.name, tc.err)
		}
	}
}

type changingPolicySource struct {
	versionCalls int
	loadCalls    int
}

func (s *changingPolicySource) PolicyVersion(context.Context) (int64, error) {
	s.versionCalls++
	if s.versionCalls == 1 {
		return 1, nil
	}
	return 2, nil
}

func (s *changingPolicySource) LoadPolicyRows(context.Context) ([]StoredPolicy, error) {
	s.loadCalls++
	permission := "system:dict:list"
	if s.loadCalls > 1 {
		permission = "system:dict:add"
	}
	return []StoredPolicy{{PType: "p", Values: []string{"realm:editor", permission}}}, nil
}

func TestStorageAdapterRetriesUntilPolicySnapshotIsStable(t *testing.T) {
	source := &changingPolicySource{}
	enforcer, err := NewEnforcer(NewStorageAdapter(source))
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if enforcer.LoadedPolicyVersion() != 2 {
		t.Fatalf("loaded version = %d, want 2", enforcer.LoadedPolicyVersion())
	}
	if allowed, err := enforcer.Allow([]string{"realm:editor"}, "system:dict:list"); err != nil || allowed {
		t.Fatalf("unstable first snapshot remained active: allowed=%v err=%v", allowed, err)
	}
	if allowed, err := enforcer.Allow([]string{"realm:editor"}, "system:dict:add"); err != nil || !allowed {
		t.Fatalf("stable second snapshot not active: allowed=%v err=%v", allowed, err)
	}
}
