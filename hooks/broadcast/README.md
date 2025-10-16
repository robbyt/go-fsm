# Broadcast Package

Provides state change notifications through channels when registered as a post-transition hook.

## Usage

```go
import (
	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
)

// Create broadcast manager
manager := broadcast.NewManager(slog.Default().Handler())

// Register as post-transition hook
registry, _ := hooks.NewRegistry(
	hooks.WithLogger(slog.Default()),
	hooks.WithTransitions(transitions.Typical),
)
registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, manager.BroadcastHook)

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
