// Package healthx 提供进程内就绪检查注册表。
package healthx

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

type Check func(context.Context) error

type entry struct {
	id    uint64
	check Check
}

type Registry struct {
	mu          sync.RWMutex
	checks      map[string]entry
	nextID      atomic.Uint64
	initialized atomic.Bool
}

func NewRegistry() *Registry { return &Registry{checks: make(map[string]entry)} }

var Default = NewRegistry()

// Register 注册或替换命名检查，并返回只删除本次注册的清理函数。
func (r *Registry) Register(name string, check Check) func() {
	id := r.nextID.Add(1)
	r.mu.Lock()
	r.checks[name] = entry{id: id, check: check}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		if current, ok := r.checks[name]; ok && current.id == id {
			delete(r.checks, name)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) MarkInitialized() { r.initialized.Store(true) }

func (r *Registry) Ready(ctx context.Context) error {
	if !r.initialized.Load() {
		return errors.New("service is still initializing")
	}
	r.mu.RLock()
	names := make([]string, 0, len(r.checks))
	checks := make(map[string]Check, len(r.checks))
	for name, item := range r.checks {
		names = append(names, name)
		checks[name] = item.check
	}
	r.mu.RUnlock()
	sort.Strings(names)
	var errs []error
	for _, name := range names {
		if err := checks[name](ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
