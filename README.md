# go-fsm

[![Go Reference](https://pkg.go.dev/badge/github.com/robbyt/go-fsm.svg)](https://pkg.go.dev/github.com/robbyt/go-fsm)
[![Go Report Card](https://goreportcard.com/badge/github.com/robbyt/go-fsm)](https://goreportcard.com/report/github.com/robbyt/go-fsm)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=robbyt_go-fsm&metric=coverage)](https://sonarcloud.io/summary/new_code?id=robbyt_go-fsm)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A thread-safe finite state machine implementation for Go that supports custom states, transitions, and state change notifications.

## Features

- Define custom states and allowed transitions
- Thread-safe state management using atomic operations
- Functional hook callbacks (pre-transition guards, entry/exit actions)
- Subscribe to state changes via channels with context support
- Structured logging with `log/slog`

## Installation

```bash
go get github.com/robbyt/go-fsm
```

## Quick Start

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/robbyt/go-fsm"
)

func main() {
	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a new FSM with initial state and predefined transitions
	machine, err := fsm.New(logger.Handler(), fsm.StatusNew, fsm.TypicalTransitions)
	if err != nil {
		logger.Error("failed to create FSM", "error", err)
		return
	}

	// Subscribe to state changes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	stateChan := machine.GetStateChan(ctx)
	
	go func() {
		for state := range stateChan {
			logger.Info("state changed", "state", state)
		}
	}()

	// Perform state transitions- they must follow allowed transitions
	// booting -> running -> stopping -> stopped
	if err := machine.Transition(fsm.StatusBooting); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}

	if err := machine.Transition(fsm.StatusRunning); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}

	time.Sleep(time.Second)
	
	if err := machine.Transition(fsm.StatusStopping); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}
	
	if err := machine.Transition(fsm.StatusStopped); err != nil {
		logger.Error("transition failed", "error", err)
		return
	}
}
```

## Usage

### Defining Custom States and Transitions

```go
// Define custom states
const (
	StatusOnline  = "StatusOnline"
	StatusOffline = "StatusOffline"
	StatusUnknown = "StatusUnknown"
)

// Define allowed transitions
var customTransitions = fsm.TransitionsConfig{
	StatusOnline:  []string{StatusOffline, StatusUnknown},
	StatusOffline: []string{StatusOnline, StatusUnknown},
	StatusUnknown: []string{},
}
```

### Creating an FSM

```go
// Create with default options
machine, err := fsm.New(slog.Default().Handler(), StatusOnline, customTransitions)
if err != nil {
	// Handle error
}

// Or with custom logger options
handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelDebug,
})
machine, err := fsm.New(handler, StatusOnline, customTransitions)
```

### State Transition Callbacks

The FSM implements a callback hook system following the Run-to-Completion (RTC) model. Callbacks are configured on a callback registry before passing it to the FSM.

To use callbacks, import the hooks package:

```go
import (
	"github.com/robbyt/go-fsm"
	"github.com/robbyt/go-fsm/hooks"
)
```

#### Callback Execution Order

Callbacks execute in this order during transitions:

1. **Guards** - Validate whether transition should proceed (can reject)
2. **Exit Actions** - Cleanup when leaving a state (can reject)
3. **Transition Actions** - Perform work during transition (can reject)
4. **Entry Actions** - Initialize when entering a state (cannot reject - executed after state update)
5. **Post-Transition Hooks** - Global notifications after transition completes (cannot reject)

Callbacks that can reject the transition are executed before the state update. Entry actions and post-transition hooks execute after the state is updated and cannot abort the transition.

#### Guard Conditions

Guards validate whether a transition should proceed. Multiple guards for the same transition execute in registration order (FIFO), and all must pass.

```go
// Create and configure callback registry
registry := hooks.NewSynchronousCallbackRegistry(logger)
registry.RegisterGuard(StatusOffline, StatusOnline, func(ctx context.Context, from, to string) error {
	if !networkAvailable() {
		return errors.New("network unavailable")
	}
	return nil
})

// Create FSM with callback registry
machine, err := fsm.New(logger.Handler(), StatusOffline, customTransitions,
	fsm.WithCallbackRegistry(registry),
)

// Transition will fail if guard rejects
err := machine.Transition(StatusOnline)
if errors.Is(err, hooks.ErrGuardRejected) {
	// Handle guard rejection
}
```

#### Entry Actions

Entry actions execute when entering a state after the state update. They cannot abort the transition.

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)
registry.RegisterEntryAction(StatusOnline, func(ctx context.Context, from, to string) {
	log.Printf("Entered online state from %s", from)
	startServices()
})

machine, err := fsm.New(logger.Handler(), StatusOffline, customTransitions,
	fsm.WithCallbackRegistry(registry),
)
```

#### Exit Actions

Exit actions execute when leaving a state and can prevent the transition if they fail.

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)
registry.RegisterExitAction(StatusOnline, func(ctx context.Context, from, to string) error {
	if !canShutdownSafely() {
		return errors.New("cannot exit: active connections")
	}
	return stopServices()
})

machine, err := fsm.New(logger.Handler(), StatusOnline, customTransitions,
	fsm.WithCallbackRegistry(registry),
)
```

#### Transition Actions

Transition actions execute for specific state transitions and can prevent the transition if they fail.

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)
registry.RegisterTransitionAction(StatusOffline, StatusOnline, func(ctx context.Context, from, to string) error {
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

#### Pattern Matching

Entry and exit actions support regex patterns to match multiple states:

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)

// Match all states ending in "ing"
err := registry.RegisterEntryActionPattern(".*ing$", func(ctx context.Context, from, to string) {
	log.Printf("Entered a progressive state: %s", to)
}, transitions.GetAllStates())
if err != nil {
	// Handle pattern matching error
}

// Match all states with wildcard
err = registry.RegisterExitActionPattern(".*", func(ctx context.Context, from, to string) error {
	log.Printf("Exiting state: %s", from)
	return nil
}, transitions.GetAllStates())
if err != nil {
	// Handle pattern matching error
}

machine, err := fsm.New(logger.Handler(), initialState, transitions,
	fsm.WithCallbackRegistry(registry),
)
```

Patterns are resolved during registration. If a pattern matches no states, registration fails.

#### Combining Multiple Callbacks

```go
registry := hooks.NewSynchronousCallbackRegistry(logger)

// Guard - validate transition
registry.RegisterGuard(StatusOffline, StatusOnline, func(ctx context.Context, from, to string) error {
	if !isAuthorized() {
		return errors.New("not authorized")
	}
	return nil
})

// Exit action - cleanup before leaving
registry.RegisterExitAction(StatusOffline, func(ctx context.Context, from, to string) error {
	return cleanup()
})

// Transition action - perform transition work
registry.RegisterTransitionAction(StatusOffline, StatusOnline, func(ctx context.Context, from, to string) error {
	return connect()
})

// Entry action - initialize new state
registry.RegisterEntryAction(StatusOnline, func(ctx context.Context, from, to string) {
	startServices()
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
- Guards should be pure functions with no side effects
- Long-running operations should be moved to entry actions or post-transition hooks
- Panics are recovered in all callbacks. For guards, exit actions, and transition actions, panics are returned as errors. For entry actions and post-transition hooks, panics are logged and do not propagate

### State Transitions

```go
// Simple transition
err := machine.Transition(StatusOffline)

// Conditional transition
err := machine.TransitionIfCurrentState(StatusOnline, StatusOffline)

// Get current state
currentState := machine.GetState()
```

### State Change Notifications

#### Broadcast Modes

Subscribers can operate in three different modes based on their timeout setting:

**Async Mode (timeout=0)**: State updates are dropped if the channel is full. Non-blocking transitions. This is the default behavior.

**Sync Mode (positive timeout)**: Blocks state transitions until all sync subscribers read the update or timeout. Never drops state updates unless timeout is reached.

**Infinite Blocking Mode (negative timeout)**: Blocks state transitions indefinitely until all infinite subscribers read the update. Never drops state updates or times out.

To use broadcast options, import the broadcast package:

```go
import (
	"github.com/robbyt/go-fsm"
	"github.com/robbyt/go-fsm/hooks/broadcast"
)
```

Example usage:

```go
// Get notification channel with default async behavior (timeout=0)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stateChan := machine.GetStateChan(ctx)

// Process state changes
go func() {
	for state := range stateChan {
		// Handle state change
		fmt.Println("State changed to:", state)
	}
}()

// Use sync broadcast with a 10s timeout (WithSyncBroadcast is a shortcut for settings a 10s timeout)
syncChan := machine.GetStateChanWithOptions(ctx, broadcast.WithSyncBroadcast())

// Use sync broadcast with 1hr custom timeout
timeoutChan := machine.GetStateChanWithOptions(ctx, broadcast.WithSyncTimeout(1*time.Hour))

// Use infinite blocking (never times out)
infiniteChan := machine.GetStateChanWithOptions(ctx,
	broadcast.WithSyncTimeout(-1),
)

// Read and print all state changes from the channel
go func() {
	for state := range syncChan {
		fmt.Println("State:", state)
	}
}()
```

## Complete Example

See [`example/main.go`](example/main.go) for a complete example application.

## Thread Safety

All operations on the FSM are thread-safe and can be used concurrently from multiple goroutines.

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.
