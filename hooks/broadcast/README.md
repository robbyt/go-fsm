# Broadcast Package

Provides state change notifications through channels when registered as a post-transition hook.

**Note:** For most use cases, use the built-in `machine.GetStateChan()` method instead of manually configuring a broadcast manager. This package is for advanced scenarios requiring custom broadcast logic, multiple managers, or fine-grained hook control. See the [main README](../../README.md#subscribing-to-state-changes) for the simpler approach.

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
ctx := context.Background()
stateChan, _ := manager.GetStateChan(ctx)

for state := range stateChan {
	// Handle state change
}
```

## Delivery Modes

- **Best-effort (default)**: Non-blocking, drops messages if channel is full
- **Timeout**: `WithTimeout(5*time.Second)` - blocks up to duration
- **Guaranteed**: `WithTimeout(-1)` - blocks indefinitely until delivered

See [godoc](https://pkg.go.dev/github.com/robbyt/go-fsm/v2/hooks/broadcast) for details.
