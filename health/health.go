// Package health provides HTTP liveness and readiness probes.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/eyesofblue/jgo/readiness"
)

var ErrNilMux = errors.New("health: nil ServeMux")

// Check verifies whether one dependency is ready.
type Check func(context.Context) error

// Probe serves liveness and readiness state. New probes start as not ready and
// must be marked ready after application initialization completes.
type Probe struct {
	ready    atomic.Bool
	registry *readiness.Registry
	nextID   atomic.Uint64
}

// New creates a health probe with optional readiness checks.
func New(checks ...Check) *Probe {
	return NewWithTimeout(time.Second, checks...)
}

// NewWithTimeout creates a probe whose complete dependency collection has a
// hard deadline. Checks run concurrently and panics are isolated by the shared
// readiness registry.
func NewWithTimeout(timeout time.Duration, checks ...Check) *Probe {
	probe := &Probe{registry: readiness.New(timeout)}
	for _, check := range checks {
		probe.AddReadinessCheck(check)
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
	name := fmt.Sprintf("dependency-%d", p.nextID.Add(1))
	_ = p.registry.Add(name, true, readiness.CheckFunc(check))
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

	if report := p.registry.Check(request.Context()); !report.Ready {
		write(writer, http.StatusServiceUnavailable, "not_ready")
		return
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
