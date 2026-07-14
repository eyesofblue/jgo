package readiness

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequiredAndOptionalDependencies(t *testing.T) {
	registry := New(50 * time.Millisecond)
	if err := registry.Add("database", true, CheckFunc(func(context.Context) error { return errors.New("down") })); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add("cache", false, CheckFunc(func(context.Context) error { return errors.New("down") })); err != nil {
		t.Fatal(err)
	}
	report := registry.Check(context.Background())
	if report.Ready {
		t.Fatal("required failure reported ready")
	}
	if report.Dependencies["database"].Status != "NOT_READY" || report.Dependencies["cache"].Required {
		t.Fatalf("report = %+v", report)
	}
}

func TestOptionalFailureDoesNotBlockReadiness(t *testing.T) {
	registry := New(time.Second)
	_ = registry.Add("cache", false, CheckFunc(func(context.Context) error { return errors.New("down") }))
	if report := registry.Check(context.Background()); !report.Ready {
		t.Fatalf("report = %+v", report)
	}
}

func TestCheckReturnsWhenCheckerIgnoresContext(t *testing.T) {
	registry := New(20 * time.Millisecond)
	release := make(chan struct{})
	defer close(release)
	if err := registry.Add("database", true, CheckFunc(func(context.Context) error {
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	report := registry.Check(context.Background())
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("Check blocked for %s", elapsed)
	}
	dependency := report.Dependencies["database"]
	if report.Ready || dependency.Status != "NOT_READY" || !strings.Contains(dependency.Error, "deadline exceeded") {
		t.Fatalf("report = %+v", report)
	}
}

func TestRepeatedChecksDoNotStartOverlappingStuckCheckers(t *testing.T) {
	registry := New(5 * time.Millisecond)
	release := make(chan struct{})
	defer close(release)
	var starts atomic.Int64
	if err := registry.Add("database", true, CheckFunc(func(context.Context) error {
		starts.Add(1)
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	first := registry.Check(context.Background())
	if first.Ready {
		t.Fatalf("first report = %+v", first)
	}
	for range 100 {
		report := registry.Check(context.Background())
		dependency := report.Dependencies["database"]
		if report.Ready || !strings.Contains(dependency.Error, "still running") {
			t.Fatalf("report = %+v", report)
		}
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("checker starts = %d, want 1", got)
	}
}

func TestCheckerPanicBecomesNotReady(t *testing.T) {
	registry := New(time.Second)
	if err := registry.Add(" database ", true, CheckFunc(func(context.Context) error {
		panic("connection pool corrupted")
	})); err != nil {
		t.Fatal(err)
	}
	report := registry.Check(context.Background())
	dependency, ok := report.Dependencies["database"]
	if !ok || report.Ready || dependency.Status != "NOT_READY" || !strings.Contains(dependency.Error, "panicked") {
		t.Fatalf("report = %+v", report)
	}
	if err := registry.Add("database", true, CheckFunc(func(context.Context) error { return nil })); err == nil {
		t.Fatal("trimmed duplicate dependency name was accepted")
	}
}
