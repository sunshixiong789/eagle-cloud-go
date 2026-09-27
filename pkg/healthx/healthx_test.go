package healthx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRegistryRequiresInitializationAndAggregatesChecks(t *testing.T) {
	r := NewRegistry()
	if err := r.Ready(context.Background()); err == nil {
		t.Fatal("registry should not be ready before initialization")
	}

	r.Register("database", func(context.Context) error { return nil })
	unregister := r.Register("redis", func(context.Context) error { return errors.New("unreachable") })
	r.MarkInitialized()
	if err := r.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "redis: unreachable") {
		t.Fatalf("Ready error = %v", err)
	}

	unregister()
	if err := r.Ready(context.Background()); err != nil {
		t.Fatalf("Ready after unregister = %v", err)
	}
}

func TestRegisterCleanupDoesNotDeleteReplacement(t *testing.T) {
	r := NewRegistry()
	removeOld := r.Register("database", func(context.Context) error { return errors.New("old") })
	r.Register("database", func(context.Context) error { return nil })
	removeOld()
	r.MarkInitialized()
	if err := r.Ready(context.Background()); err != nil {
		t.Fatalf("old cleanup removed replacement: %v", err)
	}
}
