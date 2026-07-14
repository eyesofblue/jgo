// Package readiness aggregates required and optional dependency checks.
package readiness

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Checker interface{ CheckReadiness(context.Context) error }
type CheckFunc func(context.Context) error

func (function CheckFunc) CheckReadiness(ctx context.Context) error { return function(ctx) }

type Dependency struct {
	Name     string `json:"-"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

type Report struct {
	Ready        bool                  `json:"ready"`
	Dependencies map[string]Dependency `json:"dependencies"`
}

type entry struct {
	name     string
	required bool
	checker  Checker
	state    *checkState
}

type checkState struct {
	mu      sync.Mutex
	running bool
}

var errCheckInProgress = fmt.Errorf("readiness: previous dependency check is still running")

func (item entry) begin() bool {
	item.state.mu.Lock()
	defer item.state.mu.Unlock()
	if item.state.running {
		return false
	}
	item.state.running = true
	return true
}

func (item entry) finish() {
	item.state.mu.Lock()
	item.state.running = false
	item.state.mu.Unlock()
}

type Registry struct {
	mu      sync.RWMutex
	timeout time.Duration
	entries map[string]entry
}

func New(timeout time.Duration) *Registry {
	if timeout <= 0 {
		timeout = time.Second
	}
	return &Registry{timeout: timeout, entries: make(map[string]entry)}
}

func (registry *Registry) Add(name string, required bool, checker Checker) error {
	if registry == nil {
		return fmt.Errorf("readiness: nil registry")
	}
	name = strings.TrimSpace(name)
	if name == "" || checker == nil {
		return fmt.Errorf("readiness: name and checker are required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.entries[name]; exists {
		return fmt.Errorf("readiness: dependency %q is already registered", name)
	}
	registry.entries[name] = entry{name: name, required: required, checker: checker, state: &checkState{}}
	return nil
}

func (registry *Registry) Check(ctx context.Context) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	registry.mu.RLock()
	entries := make([]entry, 0, len(registry.entries))
	for _, item := range registry.entries {
		entries = append(entries, item)
	}
	timeout := registry.timeout
	registry.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	report := Report{Ready: true, Dependencies: make(map[string]Dependency, len(entries))}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		entry entry
		err   error
	}
	results := make(chan result, len(entries))
	pending := make(map[string]entry, len(entries))
	for _, item := range entries {
		pending[item.name] = item
		if !item.begin() {
			results <- result{entry: item, err: errCheckInProgress}
			continue
		}
		go func(item entry) {
			defer item.finish()
			results <- result{entry: item, err: checkDependency(checkCtx, item)}
		}(item)
	}
	for len(pending) > 0 {
		select {
		case checked := <-results:
			if _, waiting := pending[checked.entry.name]; !waiting {
				continue
			}
			delete(pending, checked.entry.name)
			addResult(&report, checked.entry, checked.err)
		case <-checkCtx.Done():
			for _, item := range pending {
				addResult(&report, item, checkCtx.Err())
			}
			return report
		}
	}
	return report
}

func checkDependency(ctx context.Context, item entry) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// Readiness reports are exposed over HTTP. Do not leak panic values or
			// stack traces, which may contain implementation or secret data.
			err = fmt.Errorf("readiness: dependency %s panicked", item.name)
		}
	}()
	return item.checker.CheckReadiness(ctx)
}

func addResult(report *Report, item entry, err error) {
	dependency := Dependency{Name: item.name, Required: item.required, Status: "READY"}
	if err != nil {
		dependency.Status, dependency.Error = "NOT_READY", err.Error()
		if item.required {
			report.Ready = false
		}
	}
	report.Dependencies[item.name] = dependency
}
