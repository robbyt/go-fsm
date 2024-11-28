# Finite State Machine (FSM) Package for Go
A flexible and thread-safe finite state machine (FSM) implementation for Go. This library allows
you to define custom states and transitions, manage state changes, and subscribe to state updates
using channels.

## Features

- Define custom states and allowed transitions
- Thread-safe state management
- Subscribe to state changes via channels
- Logging support using the standard library's `log/slog`

## Installation

```bash
go get github.com/robbyt/go-fsm
```

## Usage
A full example is available in [`example/main.go`](example/main.go).

### Defining States and Transitions
Define your custom states and the allowed transitions between them. There are also some predefined
states available in [`allowedTransitions.go`](allowedTransitions.go).

```go
import (
    "github.com/robbyt/go-fsm"
)

// Define custom states
const (
    StatusOnline  = "StatusOnline"
    StatusOffline = "StatusOffline"
    StatusUnknown = "StatusUnknown"
)

// Define allowed transitions
var allowedTransitions = fsm.TransitionsConfig{
    StatusOnline:   string[]{StatusOffline, StatusUnknown},
    StatusOffline:  string[]{StatusOnline, StatusUnknown},
    StatusUnknown:  string[]{},
}
```

### Creating a New FSM
Create a new FSM instance by providing an initial state and the allowed transitions. You can also
provide a logger using the standard library's log/slog.


```go
import (
    "github.com/robbyt/go-fsm"
    "log/slog"
)

func main() {
    // Get the default logger
    logger := slog.Default()

    // Create a new FSM, with the initial state and allowed transitions from built-in states
    machine, err := fsm.New(logger.Handler(), fsm.StatusNew, fsm.TypicalTransitions)
    if err != nil {
        logger.Error("Failed to create FSM", "error", err)
        return
    }
}
```

```go
    // Alternatively, create a new handler with custom options, and pass that handler variable
    handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    )
    machine, err := fsm.New(handler, fsm.StatusNew, fsm.TypicalTransitions)
```

### Performing State Transitions
Transition between states using the Transition method. It ensures that the transition adheres to
the allowed transitions.

```go
err = machine.Transition(StateRunning)
if err != nil {
    logger.Error("Transition failed", "error", err)
}
```

### Subscribing to State Changes
Subscribe to state changes by obtaining a channel that emits the FSM's state whenever it changes.

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stateChan := machine.GetStateChan(ctx)

go func() {
    for state := range stateChan {
        logger.Info("State changed", "state", state)
    }
}()
```

## License

This project is licensed under the Apache License 2.0
