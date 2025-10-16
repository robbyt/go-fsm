# go-fsm

[![Go Reference](https://pkg.go.dev/badge/github.com/robbyt/go-fsm.svg)](https://pkg.go.dev/github.com/robbyt/go-fsm)
[![Go Report Card](https://goreportcard.com/badge/github.com/robbyt/go-fsm)](https://goreportcard.com/report/github.com/robbyt/go-fsm)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=robbyt_go-fsm&metric=coverage)](https://sonarcloud.io/summary/new_code?id=robbyt_go-fsm)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A finite state machine for that supports custom states, and pre/post transition hooks.

## Features

- Define custom states and allowed transitions
- Thread-safe state management using atomic operations
- Functional hook callbacks (pre-transition hooks, post-transition hooks)
- Subscribe to state changes via channels with context support
- Structured logging with `log/slog`

## Installation

```bash
go get github.com/robbyt/go-fsm/v2
```

## Quick Start

```go
package main

import (
	"log/slog"
	"os"

	"github.com/robbyt/go-fsm/v2"
)

func main() {
	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a new FSM with initial state and inline transitions
	machine, err := fsm.NewSimple(logger.Handler(), "new", map[string][]string{
		"new":       {"booting", "error"},
		"booting":   {"running", "error"},
		"running":   {"stopping", "error"},
		"stopping":  {"stopped", "error"},
		"stopped":   {"new", "error"},
		"error":     {},
	})
	if err != nil {
		logger.Error("failed to create FSM", "error", err)
		return
	}

	// Perform state transitions - they must follow allowed transitions
	// new -> booting -> running -> stopping -> stopped
	if err := machine.Transition("booting"); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}

	if err := machine.Transition("running"); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}

	if err := machine.Transition("stopping"); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}

	if err := machine.Transition("stopped"); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}
}
```

## Usage

### Defining Custom States and Transitions

```go
// Simple approach with inline map
machine, err := fsm.NewSimple(slog.Default().Handler(), "online", map[string][]string{
	"online":  {"offline", "unknown"},
	"offline": {"online", "unknown"},
	"unknown": {},
})

// Advanced approach with reusable transition config
customTransitions := transitions.MustNew(map[string][]string{
	"online":  {"offline", "unknown"},
	"offline": {"online", "unknown"},
	"unknown": {},
})
machine, err := fsm.New(slog.Default().Handler(), "online", customTransitions)
```

### Creating an FSM

```go
// Simple constructor with inline transitions
machine, err := fsm.NewSimple(slog.Default().Handler(), "online", map[string][]string{
	"online":  {"offline"},
	"offline": {"online"},
})
if err != nil {
	// Handle error
}

// Advanced constructor with predefined transitions
machine, err := fsm.New(slog.Default().Handler(), transitions.StatusNew, transitions.Typical)
if err != nil {
	// Handle error
}

// With custom logger options
handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelDebug,
})
machine, err := fsm.NewSimple(handler, "online", map[string][]string{
	"online":  {"offline"},
	"offline": {"online"},
})
```

### State Transition Callbacks

The FSM implements a callback hook system following the Run-to-Completion (RTC) model. Callbacks are configured on a callback registry before passing it to the FSM.

To use callbacks, import the hooks package:

```go
import (
	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
)
```

#### Callback Execution Order

Callbacks execute in this order during transitions:

1. **Validate transition is allowed** - Check if transition is defined in FSM configuration
2. **Pre-Transition Hooks** - Perform work and validation during transition (can reject)
3. **State Update** - Point of no return
4. **Post-Transition Hooks** - Global notifications after transition completes (cannot reject)

Pre-transition hooks can reject the transition by returning an error. Post-transition hooks execute after the state is updated and cannot abort the transition.

#### Pre-Transition Hooks

Pre-transition hooks execute for specific state transitions and can prevent the transition if they fail.

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)
registry.RegisterPreTransitionHook(StatusOffline, StatusOnline, func(ctx context.Context, from, to string) error {
	return establishConnection()
})

machine, err := fsm.New(logger.Handler(), StatusOffline, customTransitions,
	fsm.WithCallbackRegistry(registry),
)
```

#### Post-Transition Hooks

Post-transition hooks execute after every state transition completes. They cannot abort the transition.

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)
registry.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	metrics.RecordTransition(from, to)
})

machine, err := fsm.New(logger.Handler(), StatusOffline, customTransitions,
	fsm.WithCallbackRegistry(registry),
)
```


#### Combining Multiple Callbacks

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)

// Pre-transition hook - validate and perform transition work
registry.RegisterPreTransitionHook(StatusOffline, StatusOnline, func(ctx context.Context, from, to string) error {
	if !isAuthorized() {
		return errors.New("not authorized")
	}
	if err := cleanup(); err != nil {
		return err
	}
	return connect()
})

// Post-transition hook - global notification
registry.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	notifyStateChange(from, to)
})

machine, err := fsm.New(logger.Handler(), StatusOffline, customTransitions,
	fsm.WithCallbackRegistry(registry),
)
```

#### Performance Considerations

- Callbacks execute synchronously inside the transition lock
- Keep callbacks fast to avoid blocking other state transitions
- For best performance, validation in pre-transition hooks should be lightweight
- Long-running operations should be moved to post-transition hooks
- Panics are recovered in all callbacks. For pre-transition hooks, panics are returned as errors. For post-transition hooks, panics are logged and do not propagate

### State Transitions

```go
// Simple transition
err := machine.Transition(StatusOffline)

// Conditional transition
err := machine.TransitionIfCurrentState(StatusOnline, StatusOffline)

// Get current state
currentState := machine.GetState()
```

## Complete Example

See [`example/main.go`](example/main.go) for a complete example application.

## Thread Safety

All operations on the FSM are thread-safe and can be used concurrently from multiple goroutines.

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.
