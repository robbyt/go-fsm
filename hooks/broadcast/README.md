# Broadcast Package

The broadcast package provides state change notification functionality for go-fsm. It manages subscribers and delivers state updates to multiple channels with configurable delivery modes.

## Manual Wiring Required

**Important**: Broadcast is NOT automatically enabled when you create an FSM. You must manually register the broadcast hook to enable state change notifications.

Without registering the broadcast hook:
- `GetStateChan()` will only send the initial state
- Subsequent state transitions will NOT be broadcast to subscribers
- Your channels will appear to work but won't receive updates

## Quick Start

Here's the minimal code to enable broadcast:

```go
package main

import (
	"context"
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
)

func main() {
	logger := slog.Default()

	// Create FSM
	machine, err := fsm.New(logger.Handler(), fsm.StatusNew, fsm.transitions.TypicalTransitions)
	if err != nil {
		panic(err)
	}

	// REQUIRED: Manually register broadcast hook
	if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			machine.Broadcast.Broadcast(to)
		})
	}

	// Now GetStateChan will work
	ctx := context.Background()
	stateChan := machine.GetStateChan(ctx)

	go func() {
		for state := range stateChan {
			logger.Info("state changed", "state", state)
		}
	}()

	// Transitions will now broadcast to subscribers
	machine.Transition(fsm.transitions.StatusBooting)
	machine.Transition(fsm.transitions.StatusRunning)
}
```

## Step-by-Step Guide

### Step 1: Create the FSM

Create your FSM normally without broadcast:

```go
machine, err := fsm.New(logger.Handler(), initialState, transitions)
if err != nil {
	// Handle error
}
```

At this point, `machine.Broadcast` exists but is not connected to state transitions.

### Step 2: Access the Callback Registry

The default FSM has a `SynchronousCallbackRegistry`. You need to type assert to access it:

```go
reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry)
if !ok {
	// This only fails if you provided a custom CallbackExecutor
	panic("callback registry does not support manual broadcast registration")
}
```

Note: `machine.callbacks` is not exported, so this uses reflection-like access. This is intentional - broadcast wiring is advanced usage.

### Step 3: Register the Broadcast Hook

Register a post-transition hook that calls `machine.Broadcast.Broadcast()`:

```go
reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	machine.Broadcast.Broadcast(to)
})
```

This hook executes after every state transition and sends the new state to all subscribers.

### Step 4: Use GetStateChan

Now you can use the broadcast channels normally:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stateChan := machine.GetStateChan(ctx)

go func() {
	for state := range stateChan {
		// Handle state changes
	}
}()
```

## Complete Working Example

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Create FSM
	machine, err := fsm.New(logger.Handler(), fsm.StatusNew, fsm.transitions.TypicalTransitions)
	if err != nil {
		logger.Error("failed to create FSM", "error", err)
		return
	}

	// Register broadcast hook
	if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			machine.Broadcast.Broadcast(to)
		})
	}

	// Create subscriber
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stateChan := machine.GetStateChanWithOptions(ctx,
		broadcast.WithBufferSize(10),
	)

	// Process state changes
	go func() {
		for state := range stateChan {
			fmt.Printf("State changed: %s\n", state)
		}
	}()

	// Perform transitions
	time.Sleep(100 * time.Millisecond)
	machine.Transition(fsm.transitions.StatusBooting)

	time.Sleep(100 * time.Millisecond)
	machine.Transition(fsm.transitions.StatusRunning)

	time.Sleep(100 * time.Millisecond)
	machine.Transition(fsm.transitions.StatusStopping)

	time.Sleep(100 * time.Millisecond)
	machine.Transition(fsm.transitions.StatusStopped)

	// Wait for goroutine to finish
	time.Sleep(500 * time.Millisecond)
}
```

## Using WithCallbackRegistry Option

If you're already creating a custom callback registry for guards, entry/exit actions, etc., you can register the broadcast hook before passing the registry to the FSM:

```go
// Create and configure registry
registry := hooks.NewSynchronousCallbackRegistry(logger)

// Add your callbacks
registry.RegisterGuard(from, to, func(ctx context.Context, from, to string) error {
	return validateTransition(from, to)
})

registry.RegisterEntryAction(state, func(ctx context.Context, from, to string) {
	initializeState(state)
})

// Create FSM with registry
machine, err := fsm.New(logger.Handler(), initialState, transitions,
	fsm.WithCallbackRegistry(registry),
)

// Now register broadcast hook (must be done AFTER creating machine)
registry.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	machine.Broadcast.Broadcast(to)
})
```

**Important**: You must register the broadcast hook AFTER creating the machine because the hook needs to reference `machine.Broadcast`.

## How It Works Internally

1. **FSM Creation**: `machine.Broadcast` is initialized but not connected
2. **Hook Registration**: You register a post-transition hook that calls `machine.Broadcast.Broadcast()`
3. **State Transition**: When `machine.Transition()` is called:
   - Guards execute (can reject)
   - Exit actions execute (can reject)
   - Transition actions execute (can reject)
   - State is updated
   - Entry actions execute (cannot reject)
   - Post-transition hooks execute (cannot reject) ← **Your broadcast hook runs here**
4. **Broadcast**: The hook calls `machine.Broadcast.Broadcast(to)` which sends the new state to all subscribers

## Broadcast Modes

Subscribers can operate in three different modes based on their timeout setting:

### Async Mode (timeout=0) - Default

State updates are dropped if the channel is full. Non-blocking transitions.

```go
stateChan := machine.GetStateChan(ctx)
// or
stateChan := machine.GetStateChanWithOptions(ctx, broadcast.WithBufferSize(10))
```

### Sync Mode (positive timeout)

Blocks state transitions until all sync subscribers read the update or timeout. Never drops state updates unless timeout is reached.

```go
// 10 second timeout (shortcut)
syncChan := machine.GetStateChanWithOptions(ctx, broadcast.WithSyncBroadcast())

// Custom timeout
syncChan := machine.GetStateChanWithOptions(ctx,
	broadcast.WithSyncTimeout(1*time.Hour),
)
```

### Infinite Blocking Mode (negative timeout)

Blocks state transitions indefinitely until all infinite subscribers read the update. Never drops state updates or times out.

```go
infiniteChan := machine.GetStateChanWithOptions(ctx,
	broadcast.WithSyncTimeout(-1),
)
```

## Troubleshooting

### Channels Only Receive Initial State

**Symptom**: Your channel receives the current state when you call `GetStateChan()`, but doesn't receive any updates when transitions occur.

**Cause**: You forgot to register the broadcast hook.

**Solution**: Add the broadcast hook registration:

```go
if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
	reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
		machine.Broadcast.Broadcast(to)
	})
}
```

### Type Assertion Fails

**Symptom**: The type assertion `machine.callbacks.(*hooks.SynchronousCallbackRegistry)` returns `ok == false`.

**Cause**: You provided a custom `CallbackExecutor` via `WithCallbackRegistry()` that is not a `*SynchronousCallbackRegistry`.

**Solution**: If using a custom callback executor, implement your own mechanism to call `machine.Broadcast.Broadcast()` in your post-transition hook executor.

### Transitions Block Forever

**Symptom**: Calls to `machine.Transition()` never return.

**Cause**: You're using sync or infinite blocking mode, and your subscriber channel is full with no reader.

**Solution**:
- Ensure you have a goroutine reading from the channel
- Use a larger buffer size with `WithBufferSize()`
- Use async mode (default) if you don't need guaranteed delivery

### Race Detector Warnings

**Symptom**: `go test -race` shows data races around broadcast.

**Cause**: You're accessing `machine.Broadcast` from multiple goroutines without the broadcast hook registered.

**Solution**: Always register the broadcast hook before using any broadcast functionality.

## Advanced Usage

### Multiple Broadcast Hooks

You can register multiple post-transition hooks. They execute in FIFO order:

```go
reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	machine.Broadcast.Broadcast(to)
})

reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	metrics.RecordTransition(from, to)
})

reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	logger.Info("transition complete", "from", from, "to", to)
})
```

### Conditional Broadcasting

You can conditionally broadcast based on the transition:

```go
reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
	// Only broadcast for certain states
	if to != fsm.transitions.StatusError {
		machine.Broadcast.Broadcast(to)
	}
})
```

### Custom Callback Executors

If you implement a custom `CallbackExecutor`, you need to ensure your `ExecutePostTransitionHooks` method calls any registered broadcast hooks:

```go
type MyCustomExecutor struct {
	postHooks []func(ctx context.Context, from, to string)
}

func (e *MyCustomExecutor) ExecutePostTransitionHooks(from, to string) {
	for _, hook := range e.postHooks {
		hook(context.Background(), from, to)
	}
}

func (e *MyCustomExecutor) RegisterPostHook(hook func(ctx context.Context, from, to string)) {
	e.postHooks = append(e.postHooks, hook)
}

// Then wire it up:
executor := &MyCustomExecutor{}
machine, _ := fsm.New(logger.Handler(), initialState, transitions,
	fsm.WithCallbackRegistry(executor),
)

executor.RegisterPostHook(func(ctx context.Context, from, to string) {
	machine.Broadcast.Broadcast(to)
})
```

## Performance Considerations

- Broadcast hooks execute synchronously within the transition lock
- Keep broadcast registration simple to avoid blocking transitions
- Async mode (default) has no performance impact on transitions
- Sync and infinite blocking modes can significantly slow down transitions if subscribers are slow to read
- Consider using larger buffers for high-throughput scenarios

## See Also

- [FSM Callbacks Documentation](../../README.md#state-transition-callbacks)
- [Example Application](../../example/main.go)
- [Integration Tests](../../fsm_integration_test.go)
