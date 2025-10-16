# Broadcast Package

The broadcast package implements a post-transition hook for the FSM that notifies subscribers when state changes occur. It manages subscriber channels and delivers state updates with configurable delivery guarantees.

## Purpose

This package integrates with the FSM's hook system to provide state change notifications. When registered as a post-transition hook, it broadcasts the new state to all subscribers through their individual channels.

## Integration with FSM

The broadcast manager plugs into the FSM through the hooks registry:

```go
import (
	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
)

// Create FSM and hook registry
machine, _ := fsm.New(transitions.StatusNew, transitions.Typical,
	fsm.WithCallbacks(registry))

// Create broadcast manager
manager := broadcast.NewManager(machine.Handler())

// Register as post-transition hook for all state changes
registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"},
	manager.BroadcastHook())
```

The `BroadcastHook()` method returns a function compatible with `hooks.ActionFunc`, making it easy to register without manual wrapping.

## Subscribing to State Changes

Subscribers receive state changes through channels. The context controls the subscription lifetime:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stateChan, _ := manager.GetStateChan(ctx)

for state := range stateChan {
	// Handle state change
}
```

## Delivery Modes

The broadcast package supports three delivery modes based on the timeout configuration:

### Best-Effort (default)
Drops messages immediately if the channel is full. Non-blocking and has no performance impact on the FSM.

```go
ch, _ := manager.GetStateChan(ctx)
```

### Timeout-Based
Blocks the FSM broadcast for up to the specified duration. Drops the message if timeout is reached.

```go
ch, _ := manager.GetStateChan(ctx,
	broadcast.WithTimeout(5*time.Second),
	broadcast.WithBufferSize(10))
```

### Guaranteed Delivery
Blocks the FSM broadcast indefinitely until the message is delivered. Use when message loss is unacceptable.

```go
ch, _ := manager.GetStateChan(ctx,
	broadcast.WithTimeout(-1), // negative = wait forever
	broadcast.WithBufferSize(10))
```

## Options

- `WithBufferSize(size int)` - Creates a buffered channel
- `WithCustomChannel(ch chan string)` - Uses an external channel (caller manages lifecycle)
- `WithTimeout(duration time.Duration)` - Sets delivery behavior:
  - `timeout > 0`: blocks up to duration
  - `timeout < 0`: blocks indefinitely
  - `timeout = 0`: best-effort (default)

## Channel Lifecycle

Channels are automatically cleaned up when the context is cancelled:
- Manager-created channels are closed by the manager
- External channels (`WithCustomChannel`) are NOT closed by the manager

## Choosing a Delivery Mode

- **Best-effort**: Use for monitoring, metrics, or UI updates where occasional message loss is acceptable
- **Timeout-based**: Use when you need delivery guarantees but can't risk blocking the FSM indefinitely
- **Guaranteed delivery**: Use for auditing or when subscribers must receive every state change. Warning: slow subscribers will block all FSM state transitions.
