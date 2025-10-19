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
	"fmt"
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
)

func main() {
	logger := slog.Default()
	transitions := map[string][]string{
		"new":     {"booting"},
		"booting": {"running"},
		"running": {"stopped", "error"},
		"stopped": {"new"},
		"error":   {}, // terminal state
	}

	// Create a new FSM with an initial state and a map of allowed transitions
	machine, err := fsm.NewSimple("new", transitions, fsm.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create FSM", "error", err)
		return
	}
	fmt.Println("Initial State:", machine.GetState())

	// Transition through a series of states
	states := []string{"booting", "running", "stopped"}
	for _, state := range states {
		if err := machine.Transition(state); err != nil {
			logger.Error("transition failed", "to", state, "error", err)
			return
		}
		fmt.Printf("Transitioned to: %s\n", machine.GetState())
	}

	// This transition is not allowed and will fail
	err = machine.Transition("running") // Can't go from "stopped" to "running"
	if err != nil {
		logger.Error("invalid transition was rejected", "error", err)
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

The `transitions` package provides predefined transition sets. This example uses the `Typical` configuration.

```go
import (
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/transitions"
)

machine, err := fsm.New(
	transitions.StatusNew,
	transitions.Typical,
	fsm.WithLogger(slog.Default()),
)
if err != nil {
	// Handle error
}
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
import (
	"context"
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
)

// ...
logger := slog.Default()
registry, err := hooks.NewRegistry(
	hooks.WithLogger(logger),
	hooks.WithTransitions(customTransitions),
)
if err != nil {
	// Handle error
}

err = registry.RegisterPreTransitionHook(hooks.PreTransitionHookConfig{
	Name: "establish-connection",
	From: []string{"offline"},
	To:   []string{"online"},
	Guard: func(ctx context.Context, from, to string) error {
		return establishConnection()
	},
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
import (
	"context"
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
)

// ...
logger := slog.Default()
registry, err := hooks.NewRegistry(
	hooks.WithLogger(logger),
	hooks.WithTransitions(customTransitions),
)
if err != nil {
	// Handle error
}

err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
	Name: "record-transitions",
	From: []string{"*"},
	To:   []string{"*"},
	Action: func(ctx context.Context, from, to string) {
		metrics.RecordTransition(from, to)
	},
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
import (
	"context"
	"errors"
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
)

// ...
logger := slog.Default()
registry, err := hooks.NewRegistry(
	hooks.WithLogger(logger),
	hooks.WithTransitions(customTransitions),
)
if err != nil {
	// Handle error
}

// Pre-transition hook - validate transition
err = registry.RegisterPreTransitionHook(hooks.PreTransitionHookConfig{
	Name: "validate-offline-online",
	From: []string{"offline"},
	To:   []string{"online"},
	Guard: func(ctx context.Context, from, to string) error {
		logger.Info("validating transition", "from", from, "to", to)
		// In real code, you might check permissions, validate state, establish connections, etc.
		return nil
	},
})
if err != nil {
	// Handle error
}

// Post-transition hook - notification
err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
	Name: "notify-state-change",
	From: []string{"*"},
	To:   []string{"*"},
	Action: func(ctx context.Context, from, to string) {
		logger.Info("state changed", "from", from, "to", to)
	},
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
err = registry.RegisterPreTransitionHook(hooks.PreTransitionHookConfig{
	Name: "log-error-transitions",
	From: []string{"*"},
	To:   []string{"error"},
	Guard: func(ctx context.Context, from, to string) error {
		logger.Error("transitioning to error state", "from", from)
		return nil
	},
})

// Register a hook for all transitions FROM "running" TO any state
err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
	Name: "log-leaving-running",
	From: []string{"running"},
	To:   []string{"*"},
	Action: func(ctx context.Context, from, to string) {
		logger.Info("leaving running state", "to", to)
	},
})

// Register a hook for ALL state transitions (any from, any to)
err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
	Name: "record-all-transitions",
	From: []string{"*"},
	To:   []string{"*"},
	Action: func(ctx context.Context, from, to string) {
		metrics.RecordTransition(from, to)
	},
})
```

#### Performance Considerations

- Callbacks execute synchronously inside the FSM's transition lock.
- Keep callbacks fast to avoid blocking other state transitions.
- **Avoid long-running operations in any callback.** Since the FSM is locked during execution, slow callbacks will block all other transitions.
- If you must perform a long-running task (like I/O), do it asynchronously in a separate goroutine. These are typically launched from a post-transition hook.
- Panics are recovered in all callbacks. For pre-transition hooks, panics are returned as errors that abort the transition. For post-transition hooks, panics are logged and do not propagate.

### Subscribing to State Changes

Subscribe to state change notifications using channels. This is useful for updating UI, monitoring systems, or event-driven tasks.

#### Simple Method (Recommended)

Use the built-in `GetStateChan()` method for state notifications:

```go
import (
	"context"
	"fmt"
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/transitions"
)

func main() {
	logger := slog.Default()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Create hooks registry with transitions (required for broadcast support)
	registry, _ := hooks.NewRegistry(
		hooks.WithLogHandler(logger.Handler()),
		hooks.WithTransitions(transitions.Typical),
	)

	// 2. Create FSM with callback registry
	machine, _ := fsm.New(
		transitions.StatusNew,
		transitions.Typical,
		fsm.WithLogHandler(logger.Handler()),
		fsm.WithCallbackRegistry(registry),
	)

	// 3. Create a channel and register it
	stateChan := make(chan string, 10)
	_ = machine.GetStateChan(ctx, stateChan)

	// 4. Start listener in a goroutine
	go func() {
		for state := range stateChan {
			fmt.Println("State Update:", state)
		}
	}()

	// 5. Transitions automatically broadcast to all subscribers
	_ = machine.Transition(transitions.StatusBooting)
	_ = machine.Transition(transitions.StatusRunning)
}
```

The `GetStateChan()` method:
- Automatically sets up broadcast management
- Sends the current state immediately upon subscription
- Unsubscribes the channel when the context is cancelled
- Supports multiple concurrent subscribers

Configure broadcast timeout behavior with `fsm.WithBroadcastTimeout()`:
```go
machine, _ := fsm.New(
	transitions.StatusNew,
	transitions.Typical,
	fsm.WithBroadcastTimeout(5*time.Second), // timeout mode
	fsm.WithCallbackRegistry(registry),
)
```

Timeout values:
- `0` (default: 100ms): best-effort delivery (non-blocking)
- `> 0`: blocks up to duration, then drops message
- `< 0`: guaranteed delivery (blocks indefinitely)

#### Advanced: Custom Broadcast Manager

For advanced use cases requiring custom broadcast logic, multiple broadcast managers, or fine-grained control over hook execution order, you can manually configure a `broadcast.Manager`. See the [broadcast package documentation](hooks/broadcast/README.md) for details.

### State Transitions

There are several ways to change the FSM's state.

```go
import (
	"context"
	"time"
)

// The following examples assume this setup:
// machine, _ := fsm.NewSimple("online", map[string][]string{
// 	"online":  {"offline"},
// 	"offline": {"online"},
// })

// Transition: The standard way to change state.
// It respects the allowed transitions and executes hooks.
err := machine.Transition("offline")

// TransitionWithContext: Pass a context for cancellation or deadlines.
// The context is passed down to all hooks.
ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
defer cancel()
err = machine.TransitionWithContext(ctx, "offline")

// TransitionIfCurrentState: An atomic "compare-and-swap" operation.
// The transition only occurs if the FSM is in the expected 'from' state.
err = machine.TransitionIfCurrentState("online", "offline")

// SetState: Force the FSM to a new state, bypassing transition rules
// and pre-transition hooks. Post-transition hooks will still be executed.
// This is useful for initialization or error recovery.
err = machine.SetState("offline")

// GetState: Returns the current state.
currentState := machine.GetState()
```

## Complete Example

See [`example/main.go`](example/main.go) for a complete example application.

## Thread Safety

All operations on the FSM are thread-safe and can be used concurrently from multiple goroutines.

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.
