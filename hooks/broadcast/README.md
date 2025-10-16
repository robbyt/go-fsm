# Broadcast Package

The broadcast package provides state change notification functionality for the FSM. It manages subscribers and delivers state updates to channels with configurable delivery guarantees.

## Purpose

This package allows subscribers to receive notifications whenever the FSM transitions to a new state. Each subscriber gets their own channel that receives state updates based on their delivery preferences.

## Basic Usage

```go
package main

import (
	"context"
	"log/slog"

	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
)

func main() {
	// Create a broadcast manager
	manager := broadcast.NewManager(slog.Default())

	// Subscribe to state changes
	ctx := context.Background()
	stateChan, err := manager.GetStateChan(ctx)
	if err != nil {
		panic(err)
	}

	// Read state changes
	go func() {
		for state := range stateChan {
			slog.Info("state changed", "state", state)
		}
	}()

	// Broadcast state changes
	manager.Broadcast(transitions.StatusNew)
	manager.Broadcast(transitions.StatusBooting)
	manager.Broadcast(transitions.StatusRunning)
}
```

## Delivery Modes

The broadcast package supports three delivery modes controlled by the timeout duration:

### Best-Effort Delivery (timeout = 0, default)

Drops messages if the subscriber's channel is full. Non-blocking and has no performance impact on broadcasts.

```go
// Default mode
ch, _ := manager.GetStateChan(ctx)

// With larger buffer
ch, _ := manager.GetStateChan(ctx, broadcast.WithBufferSize(100))
```

### Delivery with Timeout (timeout > 0)

Blocks the broadcast for up to the specified duration waiting for channel space. Drops the message if the timeout is reached.

```go
ch, _ := manager.GetStateChan(ctx,
	broadcast.WithTimeout(5*time.Second),
	broadcast.WithBufferSize(10),
)
```

### Guaranteed Delivery (timeout < 0)

Blocks the broadcast indefinitely until the message is delivered. Use when message loss is unacceptable.

```go
ch, _ := manager.GetStateChan(ctx,
	broadcast.WithTimeout(-1), // negative timeout = wait forever
	broadcast.WithBufferSize(10),
)
```

## Options

All options are defined in `options.go`:

- `WithBufferSize(size int)` - Creates a channel with the specified buffer size
- `WithCustomChannel(ch chan string)` - Uses an external channel (caller manages lifecycle)
- `WithTimeout(duration time.Duration)` - Sets delivery timeout:
  - `timeout > 0`: blocks up to duration, then drops
  - `timeout < 0`: blocks indefinitely (guaranteed delivery)
  - `timeout = 0` (default): best-effort, drops immediately if full

## Channel Lifecycle

Channels are automatically cleaned up when the context is cancelled:

- **Manager-created channels** (default or `WithBufferSize`) are closed by the manager
- **External channels** (`WithCustomChannel`) are NOT closed by the manager

```go
ctx, cancel := context.WithCancel(context.Background())
ch, _ := manager.GetStateChan(ctx)

// Channel is closed when context is cancelled
cancel()
```

## Performance Considerations

- Best-effort mode (default) has no performance impact
- Timeout and guaranteed delivery modes block broadcasts until delivery or timeout
- Use larger buffers for high-throughput scenarios
- Consider your delivery requirements carefully - guaranteed delivery can block indefinitely
