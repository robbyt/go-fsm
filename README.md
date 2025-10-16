# go-fsm

[![Go Reference](https://pkg.go.dev/badge/github.com/robbyt/go-fsm.svg)](https://pkg.go.dev/github.com/robbyt/go-fsm)
[![Go Report Card](https://goreportcard.com/badge/github.com/robbyt/go-fsm)](https://goreportcard.com/report/github.com/robbyt/go-fsm)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=robbyt_go-fsm&metric=coverage)](https://sonarcloud.io/summary/new_code?id=robbyt_go-fsm)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A finite state machine that supports custom states and allowed transitions with pre/post-transition hooks for validation or notification.

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

This example creates an FSM and transitions through states:

```go
package main

import (
	"log/slog"
	"os"

	"github.com/robbyt/go-fsm/v2"
)

func main() {
	logger := slog.Default()

	// Create a new FSM with initial state and inline transitions
	machine, err := fsm.NewSimple("new", map[string][]string{
		"new":       {"booting", "error"},
		"booting":   {"running", "error"},
		"running":   {"stopping", "stopped", "error"},
		"stopping":  {"stopped", "error"},
		"stopped":   {"new", "error"},
		"error":     {}, // terminal state, no transitions out
	}, fsm.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create FSM", "error", err)
		return
	}

	// Perform state transitions following the allowed transitions defined above
	// new (initial state) -> booting -> running -> stopping -> stopped
	states := []string{"booting", "running", "stopping", "stopped"}
	for _, state := range states {
		if err := machine.Transition(state); err != nil {
			logger.Error("failed to transition", "to", state, "error", err)
			_ := machine.SetState("error") // force to error state on failure
			os.Exit(1)
		}
		fmt.Println("Current State:", machine.GetState())
	}

	err := machine.Transition("nope") // Invalid transition, state does not exist
	if err != nil {
		logger.Error("failed to transition", "to", "nope", "error", err)
	}
	fmt.Println("Final State:", machine.GetState())
}
```

## Usage

### Defining Custom States and Transitions

```go
// Simple machine definition with an inline map
machine, err := fsm.NewSimple("online", map[string][]string{
	"online":  {"offline", "error"},
	"offline": {"online", "error"},
	"error": {},
})
```

```go
// Advanced transition definition by creating a transitions object
import (
	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/transitions"
)

customTransitions := transitions.MustNew(map[string][]string{
	"online":  {"offline", "error"},
	"offline": {"online", "error"},
	"error": {},
})
machine, err := fsm.New("online", customTransitions)
```

### Creating an FSM using the "Typical" Transition Set

The transitions package provides predefined transition sets. This example uses the Typical configuration and sets a custom logger.

```go
import (
	"log/slog"
	"os"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/transitions"
)

// Advanced constructor with predefined transitions
machine, err := fsm.New(transitions.StatusNew, transitions.Typical)
if err != nil {
	// Handle error
}

// With custom logger options
handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelDebug,
})
machine, err := fsm.NewSimple("online", map[string][]string{
	"online":  {"offline"},
	"offline": {"online"},
}, fsm.WithLogHandler(handler))
```

### State Transition Callbacks

Callbacks follow the Run-to-Completion (RTC) execution model. Configure callbacks on a registry before creating the FSM.

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

Pre-transition hooks can validate or reject transitions by returning an error.

```go
logger := slog.Default()
registry, err := hooks.NewRegistry(
	hooks.WithLogger(logger),
	hooks.WithTransitions(customTransitions),
)
if err != nil {
	// Handle error
}

err = registry.RegisterPreTransitionHook([]string{"offline"}, []string{"online"}, func(ctx context.Context, from, to string) error {
	return establishConnection()
})
if err != nil {
	// Handle error
}

machine, err := fsm.New("offline", customTransitions,
	fsm.WithLogger(logger),
	fsm.WithCallbackRegistry(registry),
)
```

#### Post-Transition Hooks

Post-transition hooks execute after state changes complete and cannot reject transitions.

```go
logger := slog.Default()
registry, err := hooks.NewRegistry(
	hooks.WithLogger(logger),
	hooks.WithTransitions(customTransitions),
)
if err != nil {
	// Handle error
}

err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
	metrics.RecordTransition(from, to)
})
if err != nil {
	// Handle error
}

machine, err := fsm.New("offline", customTransitions,
	fsm.WithLogger(logger),
	fsm.WithCallbackRegistry(registry),
)
```


#### Combining Multiple Callbacks

```go
logger := slog.Default()
registry, err := hooks.NewRegistry(
	hooks.WithLogger(logger),
	hooks.WithTransitions(customTransitions),
)
if err != nil {
	// Handle error
}

// Pre-transition hook - validate and perform transition work
err = registry.RegisterPreTransitionHook([]string{"offline"}, []string{"online"}, func(ctx context.Context, from, to string) error {
	if !isAuthorized() {
		return errors.New("not authorized")
	}
	if err := cleanup(); err != nil {
		return err
	}
	return connect()
})
if err != nil {
	// Handle error
}

// Post-transition hook - global notification
err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
	notifyStateChange(from, to)
})
if err != nil {
	// Handle error
}

machine, err := fsm.New("offline", customTransitions,
	fsm.WithLogger(logger),
	fsm.WithCallbackRegistry(registry),
)
```

#### Wildcard Pattern Matching

**Important**: Wildcard patterns require the registry to be created with `hooks.WithTransitions()` option so it knows which states exist.

Use `"*"` to match any state in hook registrations.

```go
// Register a hook for all transitions FROM any state TO "error"
err = registry.RegisterPreTransitionHook([]string{"*"}, []string{"error"}, func(ctx context.Context, from, to string) error {
	logger.Error("transitioning to error state", "from", from)
	return nil
})

// Register a hook for all transitions FROM "running" TO any state
err = registry.RegisterPostTransitionHook([]string{"running"}, []string{"*"}, func(ctx context.Context, from, to string) {
	logger.Info("leaving running state", "to", to)
})

// Register a hook for ALL state transitions (any from, any to)
err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
	metrics.RecordTransition(from, to)
})
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
