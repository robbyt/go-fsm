# go-fsm

[![Go Reference](https://pkg.go.dev/badge/github.com/robbyt/go-fsm.svg)](https://pkg.go.dev/github.com/robbyt/go-fsm)
[![Go Report Card](https://goreportcard.com/badge/github.com/robbyt/go-fsm)](https://goreportcard.com/report/github.com/robbyt/go-fsm)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=robbyt_go-fsm&metric=coverage)](https://sonarcloud.io/summary/new_code?id=robbyt_go-fsm)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A thread-safe finite state machine implementation for Go that supports custom states, transitions, and state change notifications.

## Features

- Define custom states and allowed transitions
- Thread-safe state management using atomic operations
- Subscribe to state changes via channels with context support
- Structured logging via `log/slog`

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
syncChan := machine.GetStateChanWithOptions(ctx, fsm.WithSyncBroadcast())

// Use sync broadcast with 1hr custom timeout
timeoutChan := machine.GetStateChanWithOptions(ctx, fsm.WithSyncTimeout(1*time.Hour))

// Use infinite blocking (never times out)
infiniteChan := machine.GetStateChanWithOptions(ctx,
	fsm.WithSyncTimeout(-1),
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
