package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

type state uint8

const (
	stateNew state = iota
	stateRunning
	stateStopped
)

// App owns and coordinates the long-running components in a process.
// An App is safe for concurrent use, but it can only be run once.
type App struct {
	mu              sync.Mutex
	name            string
	shutdownTimeout time.Duration
	components      []Component
	componentNames  map[string]struct{}
	state           state
}

// New creates an application with safe default settings.
func New(opts ...Option) *App {
	a := &App{
		name:            "jgo",
		shutdownTimeout: defaultShutdownTimeout,
		componentNames:  make(map[string]struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// Name returns the configured application name.
func (a *App) Name() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.name
}

// Add registers a component. Components can only be added before Run starts.
func (a *App) Add(component Component) error {
	if isNilComponent(component) {
		return ErrNilComponent
	}

	name := strings.TrimSpace(component.Name())
	if name == "" {
		return ErrEmptyComponentName
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.state != stateNew {
		return ErrAppStarted
	}
	if _, exists := a.componentNames[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateComponent, name)
	}

	a.components = append(a.components, component)
	a.componentNames[name] = struct{}{}
	return nil
}

// Run starts every registered component and blocks until the parent context is
// canceled, SIGINT or SIGTERM is received, or a component exits. It then stops
// components in reverse registration order.
func (a *App) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	components, timeout, err := a.beginRun()
	if err != nil {
		return err
	}
	defer a.finishRun()

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	signalCtx, stopSignals := signal.NotifyContext(runCtx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	type startResult struct {
		component Component
		err       error
	}

	results := make(chan startResult, len(components))
	var starts sync.WaitGroup
	starts.Add(len(components))
	for _, component := range components {
		component := component
		go func() {
			defer starts.Done()
			results <- startResult{
				component: component,
				err:       startComponent(signalCtx, component),
			}
		}()
	}

	var runErr error
	select {
	case result := <-results:
		if result.err != nil {
			runErr = &ComponentError{Op: "start", Name: result.component.Name(), Err: result.err}
		}
	case <-signalCtx.Done():
		// Parent cancellation and process signals are normal shutdown paths.
	}

	cancelRun()
	stopSignals()

	shutdownErr := stopAll(components, timeout, &starts)
	return errors.Join(runErr, shutdownErr)
}

func (a *App) beginRun() ([]Component, time.Duration, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.state != stateNew {
		return nil, 0, ErrAlreadyRun
	}
	if strings.TrimSpace(a.name) == "" {
		return nil, 0, ErrInvalidName
	}
	if a.shutdownTimeout <= 0 {
		return nil, 0, ErrInvalidTimeout
	}
	if len(a.components) == 0 {
		return nil, 0, ErrNoComponents
	}

	a.state = stateRunning
	components := append([]Component(nil), a.components...)
	return components, a.shutdownTimeout, nil
}

func (a *App) finishRun() {
	a.mu.Lock()
	a.state = stateStopped
	a.mu.Unlock()
}

func stopAll(components []Component, timeout time.Duration, starts *sync.WaitGroup) error {
	done := make(chan error, 1)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		var stopErrors []error
		for i := len(components) - 1; i >= 0; i-- {
			component := components[i]
			if err := stopComponent(shutdownCtx, component); err != nil {
				stopErrors = append(stopErrors, &ComponentError{
					Op:   "stop",
					Name: component.Name(),
					Err:  err,
				})
			}
		}
		starts.Wait()
		done <- errors.Join(stopErrors...)
	}()

	select {
	case err := <-done:
		timedOut := errors.Is(shutdownCtx.Err(), context.DeadlineExceeded)
		cancel()
		if timedOut {
			return errors.Join(fmt.Errorf("%w after %s", ErrShutdownTimeout, timeout), err)
		}
		return err
	case <-shutdownCtx.Done():
		cancel()
		return fmt.Errorf("%w after %s", ErrShutdownTimeout, timeout)
	}
}

func startComponent(ctx context.Context, component Component) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	return component.Start(ctx)
}

func stopComponent(ctx context.Context, component Component) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	return component.Stop(ctx)
}

func isNilComponent(component Component) bool {
	if component == nil {
		return true
	}
	value := reflect.ValueOf(component)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
