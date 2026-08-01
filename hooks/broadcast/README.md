# Broadcast Package

Provides state change notifications through channels when registered as a post-transition hook.

**Note:** For most use cases, use the built-in `machine.Subscribe()` method instead of manually configuring a broadcast manager. This package is for advanced scenarios requiring custom broadcast logic, multiple managers, or fine-grained hook control. See the [main README](../../README.md#subscribing-to-state-changes) for the simpler approach.

## Usage

```go
import (
	"context"
	"log/slog"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
)

// Create broadcast manager
handler := slog.Default().Handler()
manager := broadcast.NewManager(handler)

// Register as post-transition hook
registry, _ := hooks.NewRegistry(
	hooks.WithLogHandler(handler),
	hooks.WithTransitions(transitions.Typical),
)
registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
	Name:   "broadcast",
	From:   []string{"*"},
	To:     []string{"*"},
	Action: manager.BroadcastHook,
})

// Create FSM
machine, _ := fsm.New(transitions.StatusNew, transitions.Typical,
	fsm.WithCallbackRegistry(registry))

// Subscribe to state changes
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stateChan, _ := manager.GetStateChan(ctx)

for state := range stateChan {
	// Handle state change; the loop ends when cancel() closes the channel.
	_ = state
}
```

## Ending a Subscription

A subscription ends when its context is cancelled, or when `UnsubscribeAll`
releases every subscriber at once:

```go
// Ends every subscription, e.g. when tearing down whatever owns this manager.
manager.UnsubscribeAll()
```

A manager-owned channel — one created by `WithBufferSize`, or by default — is
closed when its subscription ends, so a `for range` over it terminates. A channel
supplied via `WithCustomChannel` belongs to its caller: the manager never closes
it, and you must not close it while it is still subscribed.

Subscribing with a non-cancellable context such as `context.Background()` is
supported and costs nothing, but leaves `UnsubscribeAll` as the only way to end
the subscription, so a subscriber that is simply dropped stays registered for the
life of the manager. Prefer a cancellable context. An already-cancelled context
is rejected, and a channel may hold only one live subscription per manager:
subscribing the same channel twice returns an error while the first
subscription is live.

## Delivery Modes

- **Best-effort (default)**: Non-blocking, drops messages if channel is full
- **Timeout**: `WithTimeout(5*time.Second)` - blocks up to duration
- **Guaranteed**: `WithTimeout(-1)` - blocks indefinitely until delivered

> **Note:** when this manager is driven by a post-transition hook, broadcasts run
> while the FSM write lock is held. A slow subscriber therefore delays *every*
> transition for up to the configured timeout, and guaranteed delivery to a
> subscriber that never drains halts the machine entirely.

See [godoc](https://pkg.go.dev/github.com/robbyt/go-fsm/v2/hooks/broadcast) for details.
