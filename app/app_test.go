package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type testComponent struct {
	name       string
	startErr   error
	stopErr    error
	startPanic any
	stopPanic  any
	started    chan struct{}
	stopped    chan struct{}
	stopBlock  <-chan struct{}
	recordStop func(string)
	startOnce  sync.Once
	stopOnce   sync.Once
}

func newTestComponent(name string) *testComponent {
	return &testComponent{
		name:    name,
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (c *testComponent) Name() string             { return c.name }
func (c *testComponent) Started() <-chan struct{} { return c.started }

func (c *testComponent) Start(ctx context.Context) error {
	c.startOnce.Do(func() { close(c.started) })
	if c.startPanic != nil {
		panic(c.startPanic)
	}
	if c.startErr != nil {
		return c.startErr
	}
	<-ctx.Done()
	return nil
}

func (c *testComponent) Stop(ctx context.Context) error {
	if c.stopPanic != nil {
		panic(c.stopPanic)
	}
	if c.stopBlock != nil {
		select {
		case <-c.stopBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if c.recordStop != nil {
		c.recordStop(c.name)
	}
	c.stopOnce.Do(func() { close(c.stopped) })
	return c.stopErr
}

func TestAppRequiresComponents(t *testing.T) {
	err := New().Run(context.Background())
	if !errors.Is(err, ErrNoComponents) {
		t.Fatalf("Run() error = %v, want ErrNoComponents", err)
	}
}

func TestAppValidatesOptions(t *testing.T) {
	tests := []struct {
		name string
		app  *App
		want error
	}{
		{name: "empty name", app: New(WithName("  ")), want: ErrInvalidName},
		{name: "zero timeout", app: New(WithShutdownTimeout(0)), want: ErrInvalidTimeout},
		{name: "negative timeout", app: New(WithShutdownTimeout(-time.Second)), want: ErrInvalidTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.app.Add(newTestComponent("component")); err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			err := test.app.Run(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("Run() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestAddValidatesComponents(t *testing.T) {
	a := New()
	if err := a.Add(nil); !errors.Is(err, ErrNilComponent) {
		t.Fatalf("Add(nil) error = %v, want ErrNilComponent", err)
	}

	var typedNil *testComponent
	if err := a.Add(typedNil); !errors.Is(err, ErrNilComponent) {
		t.Fatalf("Add(typed nil) error = %v, want ErrNilComponent", err)
	}

	if err := a.Add(newTestComponent(" ")); !errors.Is(err, ErrEmptyComponentName) {
		t.Fatalf("Add(empty name) error = %v, want ErrEmptyComponentName", err)
	}

	if err := a.Add(newTestComponent("server")); err != nil {
		t.Fatalf("Add(server) error = %v", err)
	}
	if err := a.Add(newTestComponent("server")); !errors.Is(err, ErrDuplicateComponent) {
		t.Fatalf("Add(duplicate) error = %v, want ErrDuplicateComponent", err)
	}
}

func TestAppStopsInReverseOrder(t *testing.T) {
	var mu sync.Mutex
	var stopped []string
	record := func(name string) {
		mu.Lock()
		stopped = append(stopped, name)
		mu.Unlock()
	}

	first := newTestComponent("first")
	first.recordStop = record
	second := newTestComponent("second")
	second.recordStop = record
	third := newTestComponent("third")
	third.recordStop = record

	a := New(WithShutdownTimeout(time.Second))
	for _, component := range []Component{first, second, third} {
		if err := a.Add(component); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	waitClosed(t, first.started)
	waitClosed(t, second.started)
	waitClosed(t, third.started)
	cancel()

	if err := waitError(t, done); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"third", "second", "first"}
	if !reflect.DeepEqual(stopped, want) {
		t.Fatalf("stop order = %v, want %v", stopped, want)
	}
}

func TestComponentFailureStopsApplication(t *testing.T) {
	boom := errors.New("listen failed")
	server := newTestComponent("server")
	server.startErr = boom
	worker := newTestComponent("worker")

	a := New(WithShutdownTimeout(time.Second))
	if err := a.Add(worker); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(server); err != nil {
		t.Fatal(err)
	}

	err := a.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Run() error = %v, want wrapped start error", err)
	}
	waitClosed(t, worker.stopped)
	waitClosed(t, server.stopped)

	var componentErr *ComponentError
	if !errors.As(err, &componentErr) {
		t.Fatalf("Run() error = %T, want ComponentError", err)
	}
	if componentErr.Name != "server" || componentErr.Op != "start" {
		t.Fatalf("ComponentError = %+v", componentErr)
	}
}

func TestAppJoinsStopErrors(t *testing.T) {
	firstErr := errors.New("first stop failed")
	secondErr := errors.New("second stop failed")
	first := newTestComponent("first")
	first.stopErr = firstErr
	second := newTestComponent("second")
	second.stopErr = secondErr

	a := New(WithShutdownTimeout(time.Second))
	_ = a.Add(first)
	_ = a.Add(second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	waitClosed(t, first.started)
	waitClosed(t, second.started)
	cancel()

	err := waitError(t, done)
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("Run() error = %v, want both stop errors", err)
	}
}

func TestAppEnforcesShutdownTimeout(t *testing.T) {
	release := make(chan struct{})
	component := newTestComponent("blocked")
	component.stopBlock = release

	a := New(WithShutdownTimeout(20 * time.Millisecond))
	_ = a.Add(component)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	waitClosed(t, component.started)
	cancel()

	err := waitError(t, done)
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("Run() error = %v, want ErrShutdownTimeout", err)
	}
	close(release)
}

func TestAppCanOnlyRunOnceAndCannotBeModifiedWhileRunning(t *testing.T) {
	component := newTestComponent("server")
	a := New(WithShutdownTimeout(time.Second))
	_ = a.Add(component)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	waitClosed(t, component.started)

	if err := a.Add(newTestComponent("late")); !errors.Is(err, ErrAppStarted) {
		t.Fatalf("Add() error = %v, want ErrAppStarted", err)
	}
	cancel()
	if err := waitError(t, done); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := a.Run(context.Background()); !errors.Is(err, ErrAlreadyRun) {
		t.Fatalf("second Run() error = %v, want ErrAlreadyRun", err)
	}
}

type delayedStartupComponent struct {
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func (c *delayedStartupComponent) Name() string             { return "delayed" }
func (c *delayedStartupComponent) Started() <-chan struct{} { return c.started }
func (c *delayedStartupComponent) Start(ctx context.Context) error {
	select {
	case <-c.release:
		c.once.Do(func() { close(c.started) })
	case <-ctx.Done():
		return nil
	}
	<-ctx.Done()
	return nil
}
func (c *delayedStartupComponent) Stop(context.Context) error { return nil }

func TestProcessReadinessTracksStartupAndShutdown(t *testing.T) {
	component := &delayedStartupComponent{release: make(chan struct{}), started: make(chan struct{})}
	a := New(WithShutdownTimeout(time.Second))
	if err := a.Add(component); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	if err := a.CheckReadiness(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Fatalf("readiness before startup = %v", err)
	}
	close(component.release)
	deadline := time.Now().Add(time.Second)
	for a.CheckReadiness(context.Background()) != nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := a.CheckReadiness(context.Background()); err != nil {
		t.Fatalf("readiness after startup = %v", err)
	}
	cancel()
	if err := waitError(t, done); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckReadiness(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Fatalf("readiness after shutdown = %v", err)
	}
}

func TestAppConvertsComponentPanicsToErrors(t *testing.T) {
	component := newTestComponent("panicking")
	component.startPanic = "boom"
	a := New(WithShutdownTimeout(time.Second))
	_ = a.Add(component)

	err := a.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "panic: boom") {
		t.Fatalf("Run() error = %v, want recovered panic", err)
	}
}

func waitClosed(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close")
	}
}

func waitError(t *testing.T, channel <-chan error) error {
	t.Helper()
	select {
	case err := <-channel:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run")
		return nil
	}
}
