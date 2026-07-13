// Package health provides HTTP liveness and readiness probes.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
)

var ErrNilMux = errors.New("health: nil ServeMux")

// Check verifies whether one dependency is ready.
type Check func(context.Context) error

// Probe serves liveness and readiness state. New probes start as not ready and
// must be marked ready after application initialization completes.
type Probe struct {
	ready  atomic.Bool
	mu     sync.RWMutex
	checks []Check
}

// New creates a health probe with optional readiness checks.
func New(checks ...Check) *Probe {
	probe := &Probe{}
	for _, check := range checks {
		if check != nil {
			probe.checks = append(probe.checks, check)
		}
	}
	return probe
}

// SetReady updates the process-level readiness gate.
func (p *Probe) SetReady(ready bool) {
	p.ready.Store(ready)
}

// AddReadinessCheck adds a dependency check.
func (p *Probe) AddReadinessCheck(check Check) {
	if check == nil {
		return
	}
	p.mu.Lock()
	p.checks = append(p.checks, check)
	p.mu.Unlock()
}

// Liveness reports whether the process is alive.
func (p *Probe) Liveness(writer http.ResponseWriter, _ *http.Request) {
	write(writer, http.StatusOK, "ok")
}

// Readiness reports whether initialization and dependency checks succeed.
func (p *Probe) Readiness(writer http.ResponseWriter, request *http.Request) {
	if !p.ready.Load() {
		write(writer, http.StatusServiceUnavailable, "not_ready")
		return
	}

	p.mu.RLock()
	checks := append([]Check(nil), p.checks...)
	p.mu.RUnlock()
	for _, check := range checks {
		if err := check(request.Context()); err != nil {
			write(writer, http.StatusServiceUnavailable, "not_ready")
			return
		}
	}
	write(writer, http.StatusOK, "ok")
}

// Register adds GET /healthz and GET /readyz handlers to mux.
func (p *Probe) Register(mux *http.ServeMux) error {
	if mux == nil {
		return ErrNilMux
	}
	mux.HandleFunc("GET /healthz", p.Liveness)
	mux.HandleFunc("GET /readyz", p.Readiness)
	return nil
}

type result struct {
	Status string `json:"status"`
}

func write(writer http.ResponseWriter, status int, value string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(result{Status: value})
}
