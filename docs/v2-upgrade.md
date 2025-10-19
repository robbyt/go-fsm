# Upgrading from go-fsm v1 to v2: LLM Migration Guide

This document provides a migration prompt for Large Language Models to help migrate Go codebases from `github.com/robbyt/go-fsm` v1 to v2.

## Quick Reference: Major API Changes

| Feature | v1 API | v2 API |
|---------|--------|--------|
| **Package** | `github.com/robbyt/go-fsm` | `github.com/robbyt/go-fsm/v2` |
| **Constructor** | `fsm.New(handler, initialState, transitions)` | `fsm.New(initialState, transitions, opts...)` |
| **Logger Setup** | Required as first parameter to `New` constructor | Optional via `fsm.WithLogHandler(handler)` |
| **State Constants** | `fsm.StatusNew`, `fsm.StatusBooting`, etc. | `transitions.StatusNew`, `transitions.StatusBooting`, etc. |
| **Transitions Type** | `map[string][]string` (also `fsm.TypicalTransitions`) | `*transitions.Config` (use `transitions.Typical` for common transitions) |
| **State Broadcasting** | `stateChan := machine.GetStateChan(ctx)` | `err := machine.GetStateChan(ctx, chan)` with a user-created channel |
| **Broadcast Timeout** | `fsm.WithSyncTimeout(duration)` | `fsm.WithBroadcastTimeout(duration)` option |
| **Hooks/Callbacks** | Not supported | New `hooks.Registry` with pre/post hooks |

## What's New in v2

**Key Feature:** v2 provides a **built-in helper method** for state broadcasting that's simpler than manual setup:

- ✅ **One Method Call**: `machine.GetStateChan(ctx, chan)` handles everything
- ✅ **Automatic Hook Registration**: Broadcast hook registered automatically on first call
- ✅ **v1 Compatible**: Sends initial state immediately (just like v1)
- ✅ **You Control the Channel**: Create buffered or unbuffered channels as needed
- ✅ **Clean Error Handling**: Returns errors instead of panicking

**Migration Impact**: Most codebases can use the built-in method instead of manually wiring up `broadcast.Manager` + `hooks.Registry`. This simplifies migration significantly.

**When You Need Advanced Control**: You can use `broadcast.Manager` directly for custom broadcast logic, multiple managers, or fine-grained control over hook execution order.

## Migration Instructions for LLMs

When upgrading a codebase from go-fsm v1 to v2, follow these steps:

### Step 1: Analyze Current v1 Usage

First, search the codebase for v1 usage patterns:

```bash
# Find all imports
grep -r "github.com/robbyt/go-fsm" --include="*.go"

# Find all fsm.New() calls
grep -r "fsm\.New" --include="*.go"

# Find all GetStateChan usage
grep -r "GetStateChan" --include="*.go"

# Find status constant usage
grep -r "fsm\.Status" --include="*.go"
```

Document the following:
- How many places create FSM instances?
- Is `GetStateChan` used? Where and how?
- Are custom transitions used or `fsm.TypicalTransitions`?
- Are there any options like `WithSyncTimeout`?

### Step 2: Update go.mod

Replace the v1 dependency:

```go
// Before
require (
    github.com/robbyt/go-fsm v1.5.0
)

// After
require (
    github.com/robbyt/go-fsm/v2 v2.0.0
)
```

Then run:
```bash
go mod tidy
```

### Step 3: Update Import Statements

Update all imports in `.go` files:

```go
// Before
import "github.com/robbyt/go-fsm"

// After - may need multiple imports
import (
    "github.com/robbyt/go-fsm/v2"
    "github.com/robbyt/go-fsm/v2/transitions"
    "github.com/robbyt/go-fsm/v2/hooks"           // if using callbacks
    "github.com/robbyt/go-fsm/v2/hooks/broadcast" // if using GetStateChan
)
```

### Step 4: Update State Constants

Replace all state constant references:

```go
// Before
const (
    StatusNew     = fsm.StatusNew
    StatusBooting = fsm.StatusBooting
    // etc.
)

// After
const (
    StatusNew     = transitions.StatusNew
    StatusBooting = transitions.StatusBooting
    // etc.
)
```

Or directly use `transitions.StatusX` throughout the code.

### Step 5: Update Transition Tables

If using `fsm.TypicalTransitions`:

```go
// Before
var TypicalTransitions = fsm.TypicalTransitions

// After
var TypicalTransitions = transitions.Typical
```

For custom transition maps:

```go
// Before
customTrans := map[string][]string{
    "online":  {"offline", "error"},
    "offline": {"online", "error"},
    "error":   {},
}

// After - wrap in transitions.Config
customTrans := transitions.MustNew(map[string][]string{
    "online":  {"offline", "error"},
    "offline": {"online", "error"},
    "error":   {},
})
```

### Step 6: Update FSM Constructor Calls

Transform `fsm.New()` calls to use the v2 signature:

```go
// Before (v1)
machine, err := fsm.New(handler, fsm.StatusNew, fsm.TypicalTransitions)

// After (v2) - basic version
machine, err := fsm.New(
    transitions.StatusNew,
    transitions.Typical,
    fsm.WithLogHandler(handler),
)
```

**Important:** The logger is now optional and passed via functional option.

### Step 7: Migrate GetStateChan Usage (Critical)

This changed significantly in v2. The v2 API provides a built-in helper method on the FSM.

#### Decision Guide: Which Broadcasting Pattern Should You Use?

```
Do you need state change notifications?
│
├─ NO → Skip this step, use fsm.New() without hooks.Registry
│
└─ YES → Choose your approach:
    │
    ├─ SIMPLE (Recommended for 90% of use cases)
    │  ✅ Use: machine.GetStateChan(ctx, chan)
    │  ✅ When: Standard broadcasting, single FSM, v1 compatibility
    │  ✅ Benefits: Automatic setup, simpler code, less boilerplate
    │
    └─ ADVANCED (Only when you need special control)
       ✅ Use: broadcast.Manager directly
       ✅ When: Multiple broadcast managers, custom delivery logic,
               fine-grained hook ordering, or managing broadcasts
               across multiple FSMs
       ✅ Tradeoff: More code, manual hook registration
```

**👉 Start with the SIMPLE pattern below. Only use ADVANCED if you have specific requirements.**

#### v1 Pattern:
```go
// v1 code
stateChan := machine.GetStateChan(ctx)
// or
stateChan := machine.GetStateChanWithOptions(ctx, fsm.WithSyncTimeout(5*time.Second))

// Channel immediately receives current state, then receives all future changes
for state := range stateChan {
    // Handle state
}
```

#### v2 Pattern (SIMPLE - Use This):
```go
// v2 code - simpler pattern using built-in helper

// 1. Create a hooks registry with transitions (required for broadcast)
registry, err := hooks.NewRegistry(
    hooks.WithLogHandler(handler),
    hooks.WithTransitions(transitions.Typical),
)

// 2. Create FSM with the registry
machine, err := fsm.New(
    transitions.StatusNew,
    transitions.Typical,
    fsm.WithLogHandler(handler),
    fsm.WithCallbackRegistry(registry),
    fsm.WithBroadcastTimeout(5*time.Second), // Optional: configure broadcast timeout
)

// 3. Create your channel (you control buffer size)
stateChan := make(chan string, 10)

// 4. Register the channel with GetStateChan
err = machine.GetStateChan(ctx, stateChan)
if err != nil {
    // Handle error
}

// Channel immediately receives current state (v1 compatible!)
// Then receives all future state changes
for state := range stateChan {
    // Handle state
}
```

#### v2 Pattern (Advanced - Using broadcast.Manager directly):
If you need more control, you can still use the broadcast manager directly:

```go
// 1. Create a broadcast manager
broadcastManager := broadcast.NewManager(handler)

// 2. Create a hooks registry
registry, err := hooks.NewRegistry(
    hooks.WithLogHandler(handler),
    hooks.WithTransitions(transitions.Typical),
)

// 3. Register broadcast as a post-transition hook
err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
    Name:   "broadcast",
    From:   []string{"*"},
    To:     []string{"*"},
    Action: broadcastManager.BroadcastHook,
})

// 4. Create FSM with the registry
machine, err := fsm.New(
    transitions.StatusNew,
    transitions.Typical,
    fsm.WithLogHandler(handler),
    fsm.WithCallbackRegistry(registry),
)

// 5. Get state channel
stateChan, err := broadcastManager.GetStateChan(ctx, broadcast.WithTimeout(5*time.Second))

// 6. IMPORTANT: broadcast.Manager does NOT send initial state automatically
// You must manually broadcast the initial state if needed (for v1 compatibility)
broadcastManager.Broadcast(machine.GetState())

// Now use the channel
for state := range stateChan {
    // Handle state
}
```

#### Key Changes from v1:

1. **Channel Creation**: You create and own the channel (control buffer size)
2. **Registry Required**: Must use `hooks.Registry` with `WithTransitions()`
3. **v1 Compatible**: `machine.GetStateChan()` sends initial state immediately (like v1)
4. **Configurable Timeout**: Use `WithBroadcastTimeout()` option (replaces v1's `WithSyncTimeout`)
5. **Error Handling**: Returns error instead of just returning a channel

### Step 8: Create Abstraction Layer (Recommended)

**Best Practice:** Use a single local abstraction constructor to centralize all v2 setup complexity.

Instead of repeating the broadcast manager + hooks registry setup everywhere you create an FSM, encapsulate it in ONE constructor function. This approach:

- **Reduces duplication** - Setup code appears only once
- **Prevents errors** - No risk of forgetting to wire up broadcast hooks in different places
- **Simplifies maintenance** - Future changes (like adding new hooks) happen in one location
- **Maintains clean architecture** - Rest of codebase uses v1-like API
- **Makes testing easier** - Single point to mock or configure for tests

**When to use:** If your architecture allows for a shared internal package (e.g., `internal/finitestate`), this is the recommended approach for any codebase beyond trivial size.

If migrating a large codebase, create a compatibility wrapper:

```go
package yourinternalpackage

import (
    "context"
    "log/slog"

    "github.com/robbyt/go-fsm/v2"
    "github.com/robbyt/go-fsm/v2/hooks"
    "github.com/robbyt/go-fsm/v2/hooks/broadcast"
    "github.com/robbyt/go-fsm/v2/transitions"
)

// Re-export state constants
const (
    StatusNew       = transitions.StatusNew
    StatusBooting   = transitions.StatusBooting
    StatusRunning   = transitions.StatusRunning
    StatusReloading = transitions.StatusReloading
    StatusStopping  = transitions.StatusStopping
    StatusStopped   = transitions.StatusStopped
    StatusError     = transitions.StatusError
    StatusUnknown   = transitions.StatusUnknown
)

// Re-export transitions
var TypicalTransitions = transitions.Typical

// Machine wraps v2 FSM with v1-compatible API
type Machine struct {
    *fsm.Machine
}

// GetStateChan maintains v1 behavior - sends current state immediately
func (m *Machine) GetStateChan(ctx context.Context) <-chan string {
    ch := make(chan string, 10)

    err := m.Machine.GetStateChan(ctx, ch)
    if err != nil {
        close(ch)
        return ch
    }

    // machine.GetStateChan already sends current state immediately (v1 compatible!)
    return ch
}

// New creates a new FSM with v1-like API
func New(handler slog.Handler) (*Machine, error) {
    registry, err := hooks.NewRegistry(
        hooks.WithLogHandler(handler),
        hooks.WithTransitions(TypicalTransitions),
    )
    if err != nil {
        return nil, err
    }

    f, err := fsm.New(
        StatusNew,
        TypicalTransitions,
        fsm.WithLogHandler(handler),
        fsm.WithCallbackRegistry(registry),
    )
    if err != nil {
        return nil, err
    }

    return &Machine{
        Machine: f,
    }, nil
}
```

**Key Point:** This wrapper provides ONE centralized constructor (`New()`) that handles all the v2 complexity. The rest of your codebase calls `finitestate.New(handler)` instead of directly using v2 APIs. This maintains the v1 API surface while using v2 internally, minimizing changes to the rest of the codebase.

With this abstraction layer:
```go
// Everywhere in your codebase:
machine, err := finitestate.New(handler)
stateChan := machine.GetStateChan(ctx)

// Instead of repeating 20+ lines of v2 setup code in every location
```

This is the recommended pattern if your project architecture supports an internal/shared package.

### Step 9: Update Tests

Review and update tests:

```go
// Before (v1)
import "github.com/robbyt/go-fsm"

func TestStateMachine(t *testing.T) {
    machine, err := fsm.New(handler, fsm.StatusNew, fsm.TypicalTransitions)
    assert.NoError(t, err)
    assert.Equal(t, fsm.StatusNew, machine.GetState())
}

// After (v2)
import "github.com/robbyt/go-fsm/v2/transitions"

func TestStateMachine(t *testing.T) {
    machine, err := fsm.New(
        transitions.StatusNew,
        transitions.Typical,
        fsm.WithLogHandler(handler),
    )
    assert.NoError(t, err)
    assert.Equal(t, transitions.StatusNew, machine.GetState())
}
```

If you created a wrapper, tests using the wrapper need minimal changes:

```go
// With abstraction layer
import "yourproject/internal/finitestate"

func TestStateMachine(t *testing.T) {
    machine, err := finitestate.New(handler)
    assert.NoError(t, err)
    assert.Equal(t, finitestate.StatusNew, machine.GetState())
}
```

### Step 10: Verify the Migration

Run these commands to verify the upgrade:

```bash
# Update dependencies
go mod tidy

# Verify compilation
go build ./...

# Run all tests
go test ./...

# Run tests with race detection
go test -race ./...

# Check for any remaining v1 imports
grep -r "github.com/robbyt/go-fsm\"" --include="*.go"
# Should return no results (only v2 imports with /v2)
```

## Common Patterns & Examples

### Pattern 1: Simple State Machine (No Broadcasting)

```go
// v1
machine, err := fsm.New(handler, "idle", map[string][]string{
    "idle":    {"working"},
    "working": {"idle", "error"},
    "error":   {},
})

// v2
machine, err := fsm.NewSimple("idle", map[string][]string{
    "idle":    {"working"},
    "working": {"idle", "error"},
    "error":   {},
}, fsm.WithLogHandler(handler))
```

### Pattern 2: Using Standard Transitions

```go
// v1
machine, err := fsm.New(handler, fsm.StatusNew, fsm.TypicalTransitions)

// v2
machine, err := fsm.New(
    transitions.StatusNew,
    transitions.Typical,
    fsm.WithLogHandler(handler),
)
```

### Pattern 3: With State Broadcasting

```go
// v1
machine, err := fsm.New(handler, fsm.StatusNew, fsm.TypicalTransitions)
stateChan := machine.GetStateChan(ctx)

// v2 - using built-in GetStateChan helper
registry, err := hooks.NewRegistry(
    hooks.WithLogHandler(handler),
    hooks.WithTransitions(transitions.Typical),
)

machine, err := fsm.New(
    transitions.StatusNew,
    transitions.Typical,
    fsm.WithLogHandler(handler),
    fsm.WithCallbackRegistry(registry),
)

stateChan := make(chan string, 10)
err = machine.GetStateChan(ctx, stateChan)

// Use the channel
for state := range stateChan {
    // Handle state changes
}
```

### Pattern 4: Custom Transitions with Type Safety

```go
// v2 only - new pattern
import "github.com/robbyt/go-fsm/v2/transitions"

customTrans := transitions.MustNew(map[string][]string{
    "draft":     {"review", "archived"},
    "review":    {"approved", "rejected", "draft"},
    "approved":  {"published", "archived"},
    "rejected":  {"draft", "archived"},
    "published": {"archived"},
    "archived":  {},
})

machine, err := fsm.New("draft", customTrans)
```

## Troubleshooting

### Error: "undefined: fsm.StatusNew"
**Solution:** Import `transitions` package and use `transitions.StatusNew`

### Error: "cannot use map[string][]string as type transitionDB"
**Solution:** Wrap map with `transitions.MustNew(yourMap)` or use `transitions.New(yourMap)`

### Error: "GetStateChan requires a callback registry"
**Solution:** Use `fsm.WithCallbackRegistry(registry)` when creating the FSM. The registry must be created with `hooks.WithTransitions()` for wildcard support.

### Error: "requires a callback registry that supports dynamic hook registration"
**Solution:** Use `hooks.Registry` instead of a custom CallbackExecutor. The FSM's built-in `GetStateChan` requires the registry to support dynamic hook registration.

### Error: "wildcard '*' cannot be used without state table"
**Solution:** When using wildcard hooks (`"*"`), you must pass `hooks.WithTransitions()` to the registry.

## Additional Resources

- [go-fsm v2 Documentation](https://pkg.go.dev/github.com/robbyt/go-fsm/v2)
- [go-fsm v2 Examples](https://github.com/robbyt/go-fsm/tree/main/example)
- [go-fsm v2 README](https://github.com/robbyt/go-fsm/blob/main/README.md)

## Summary Checklist

Use this checklist to verify your migration:

- [ ] Updated `go.mod` to use v2
- [ ] Updated all imports to `/v2` variants
- [ ] Replaced `fsm.StatusX` with `transitions.StatusX`
- [ ] Replaced `fsm.TypicalTransitions` with `transitions.Typical`
- [ ] Updated `fsm.New()` constructor calls (moved handler to options)
- [ ] Migrated `GetStateChan()` to use `machine.GetStateChan(ctx, chan)` with hooks.Registry
- [ ] Added `WithBroadcastTimeout()` option if custom timeout needed (replaces `WithSyncTimeout`)
- [ ] **Created single abstraction constructor (if architecture supports it)**
- [ ] Updated all tests
- [ ] Verified with `go build ./...`
- [ ] Verified with `go test ./...`
- [ ] Verified with `go test -race ./...`
- [ ] Removed all v1 imports (verify with grep)

---

**Note:** This guide is designed for LLM consumption. When using this guide, an LLM should analyze the specific codebase context and apply these patterns appropriately.

**Architecture Recommendation:** The single abstraction constructor pattern (Step 8) is recommended if your project architecture supports it. Instead of scattering v2 setup code throughout your codebase, encapsulate ALL FSM creation logic in ONE internal constructor function. This centralizes complexity, prevents errors, and makes your codebase more maintainable. Future hook additions or configuration changes only require updating a single function rather than hunting down every FSM instantiation site.
