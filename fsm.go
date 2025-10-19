/*
Copyright 2024 Robert Terhaar <robbyt@robbyt.net>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package fsm provides a thread-safe finite state machine implementation.
// It allows defining custom states and transitions, managing state changes,
// subscribing to state updates via channels, and persisting/restoring state via JSON.
//
// Example usage:
//
//	machine, err := fsm.NewSimple("new", map[string][]string{
//	    "new":     {"running"},
//	    "running": {"stopped"},
//	    "stopped": {},
//	})
//	if err != nil {
//	    return err
//	}
//
//	err = machine.Transition("running")
//	if err != nil {
//	    return err
//	}
package fsm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
)

type transitionDB interface {
	HasState(state string) bool
	GetAllStates() []string
	IsTransitionAllowed(from, to string) bool
}

// Machine represents a finite state machine that tracks its current state
// and manages state transitions.
type Machine struct {
	mutex sync.RWMutex
	state atomic.Value

	transitions transitionDB
	callbacks   CallbackExecutor
	logger      *slog.Logger

	broadcastManager  *broadcast.Manager
	broadcastTimeout  time.Duration
	stateChanSetup    sync.Once
	stateChanSetupErr error
}

// New creates a finite state machine with the specified initial state and transitions.
// For simpler usage with inline maps, see NewSimple().
//
// Example usage:
//
//	trans := transitions.MustNew(map[string][]string{
//	    transitions.StatusNew:     {transitions.StatusBooting, transitions.StatusError},
//	    transitions.StatusBooting: {transitions.StatusRunning, transitions.StatusError},
//	    transitions.StatusRunning: {transitions.StatusReloading, transitions.StatusStopping, transitions.StatusError},
//	})
//	machine, err := fsm.New(transitions.StatusNew, trans)
//
// Or use the provided transitions.Typical:
//
//	machine, err := fsm.New(transitions.StatusNew, transitions.Typical)
func New(
	initialState string,
	trans transitionDB,
	opts ...Option,
) (*Machine, error) {
	handler := slog.Default().
		Handler().
		WithGroup("fsm")

	if trans == nil {
		return nil, fmt.Errorf("%w: transitions is nil", ErrInvalidConfiguration)
	}

	if !trans.HasState(initialState) {
		return nil, fmt.Errorf(
			"%w: initial state '%s' is not defined in transitions",
			ErrInvalidConfiguration,
			initialState,
		)
	}

	m := &Machine{
		transitions:      trans,
		logger:           slog.New(handler),
		broadcastTimeout: 100 * time.Millisecond,
	}
	m.state.Store(initialState)

	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	return m, nil
}

// NewSimple creates a FSM with a map-based transition configuration.
//
// Example usage:
//
//	machine, err := fsm.NewSimple("online", map[string][]string{
//	    "online":  {"offline", "error"},
//	    "offline": {"online", "error"},
//	    "error":   {},
//	})
func NewSimple(
	initialState string,
	allowedTransitions map[string][]string,
	opts ...Option,
) (*Machine, error) {
	trans, err := transitions.New(allowedTransitions)
	if err != nil {
		return nil, err
	}
	return New(initialState, trans, opts...)
}

// GetState returns the current state of the finite state machine.
func (fsm *Machine) GetState() string {
	return fsm.state.Load().(string)
}

// GetAllStates returns all allowed states that have been added to this FSM.
func (fsm *Machine) GetAllStates() []string {
	fsm.mutex.RLock()
	defer fsm.mutex.RUnlock()
	return fsm.transitions.GetAllStates()
}

// SetState updates the FSM's state, bypassing transition rules and pre-transition hooks.
// Returns an error if the state is not defined in the transition table.
func (fsm *Machine) SetState(state string) error {
	return fsm.SetStateWithContext(context.Background(), state)
}

// SetStateWithContext updates the FSM's state with a context, bypassing transition rules and pre-transition hooks.
// Returns an error if the state is not defined in the transition table.
func (fsm *Machine) SetStateWithContext(ctx context.Context, state string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("set state canceled: %w", err)
	}

	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	if !fsm.transitions.HasState(state) {
		return fmt.Errorf(
			"%w: state '%s' is not defined in transitions",
			ErrInvalidConfiguration,
			state,
		)
	}

	fromState := fsm.GetState()
	fsm.setState(state)
	fsm.logger.Debug("Set state", "from", fromState, "to", state)

	if fsm.callbacks != nil {
		fsm.callbacks.ExecutePostTransitionHooks(ctx, fromState, state)
	}

	return nil
}

// setState updates the FSM's state atomically.
// Assumes the caller holds the write lock.
func (fsm *Machine) setState(state string) {
	fsm.state.Store(state)
}

// Transition changes the FSM's state to toState if the transition is allowed.
// Returns an error if the transition is not permitted.
func (fsm *Machine) Transition(toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(context.Background(), toState)
}

// TransitionBool returns true if the transition to toState succeeds, false otherwise.
func (fsm *Machine) TransitionBool(toState string) bool {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(context.Background(), toState) == nil
}

// TransitionWithContext changes the FSM's state to toState with a context.
// The context is passed to all hooks for request-scoped values, tracing, or cancellation.
// Returns an error if the transition is not permitted.
func (fsm *Machine) TransitionWithContext(ctx context.Context, toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(ctx, toState)
}

// TransitionBoolWithContext returns true if the transition to toState succeeds with a context, false otherwise.
func (fsm *Machine) TransitionBoolWithContext(ctx context.Context, toState string) bool {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(ctx, toState) == nil
}

// TransitionIfCurrentState transitions to toState only if the current state matches fromState.
// Returns an error if the current state does not match or if the transition is not allowed.
func (fsm *Machine) TransitionIfCurrentState(fromState, toState string) error {
	return fsm.TransitionIfCurrentStateWithContext(context.Background(), fromState, toState)
}

// TransitionIfCurrentStateWithContext transitions to toState with a context only if the current state matches fromState.
// Returns an error if the current state does not match or if the transition is not allowed.
func (fsm *Machine) TransitionIfCurrentStateWithContext(ctx context.Context, fromState, toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	currentState := fsm.GetState()
	if currentState != fromState {
		return fmt.Errorf(
			"%w: current state is '%s', expected '%s' for transition to '%s'",
			ErrCurrentStateIncorrect,
			currentState,
			fromState,
			toState,
		)
	}

	return fsm.transition(ctx, toState)
}

// transition changes the FSM's state from the current state to toState.
// Assumes the caller holds the write lock.
// The context is passed to all hooks for request-scoped values, tracing, and cancellation handling.
// Hooks are responsible for checking context cancellation themselves if needed.
func (fsm *Machine) transition(ctx context.Context, toState string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("transition canceled: %w", err)
	}

	currentState := fsm.GetState()
	if !fsm.transitions.IsTransitionAllowed(currentState, toState) {
		return fmt.Errorf(
			"%w: transition from '%s' to '%s' is not allowed",
			ErrInvalidStateTransition,
			currentState, toState,
		)
	}

	if fsm.callbacks != nil {
		fsm.logger.Debug("Executing pre-transition hooks...", "from", currentState, "to", toState)
		if err := fsm.callbacks.ExecutePreTransitionHooks(ctx, currentState, toState); err != nil {
			fsm.logger.Debug("Pre-transition hooks failed, aborting the transition", "from", currentState, "to", toState, "error", err)
			return err
		}
		fsm.logger.Debug("Pre-transition hooks completed", "from", currentState, "to", toState)
	}

	fsm.setState(toState)
	fsm.logger.Debug("Transition successful", "from", currentState, "to", toState)

	if fsm.callbacks != nil {
		fsm.logger.Debug("Executing post-transition hooks...", "from", currentState, "to", toState)
		fsm.callbacks.ExecutePostTransitionHooks(ctx, currentState, toState)
		fsm.logger.Debug("Post-transition hooks completed", "from", currentState, "to", toState)
	}

	return nil
}

// GetStateChan registers a channel to receive state change notifications and sends
// the current state immediately, blocking on sending the initial state if the channel buffer is full.
// Use a buffered channel to avoid blocking.
//
// This method requires the FSM to be configured with a hooks.Registry (via WithCallbackRegistry).
// The registry must be created with WithTransitions() to support wildcard pattern matching.
// Returns an error if the callback executor does not support dynamic hook registration.
//
// The channel will automatically receive all future state transitions until the context
// is cancelled, at which point it is automatically unsubscribed from receiving further broadcasts.
// The channel is NOT closed; the caller maintains ownership and is responsible for channel lifecycle.
//
// GetStateChan can be called multiple times with different channels and contexts. All channels
// share the same broadcast manager, which is lazily initialized only once upon the first call.
//
// Broadcast delivery behavior is controlled by the timeout configured via WithBroadcastTimeout:
//   - timeout = 0: best-effort delivery (non-blocking, drops if channel is full)
//   - timeout > 0: blocks up to the timeout duration, then drops the message
//   - timeout < 0: guaranteed delivery (blocks indefinitely until delivered)
//
// Default timeout is 100ms if not configured.
//
// The channel must be bidirectional (chan string, not <-chan string) to allow the FSM
// to send state updates. The caller maintains ownership of the channel and controls
// its buffer size and lifecycle.
//
// Example:
//
//	registry, _ := hooks.NewRegistry(
//	    hooks.WithLogHandler(handler),
//	    hooks.WithTransitions(transitions.Typical),
//	)
//
//	machine, _ := fsm.New(
//	    transitions.StatusNew,
//	    transitions.Typical,
//	    fsm.WithCallbackRegistry(registry),
//	)
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	stateChan := make(chan string, 10)
//	if err := machine.GetStateChan(ctx, stateChan); err != nil {
//	    return err
//	}
//
//	for state := range stateChan {
//	    log.Printf("State: %s", state)
//	}
func (fsm *Machine) GetStateChan(ctx context.Context, c chan string) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	if fsm.callbacks == nil {
		return fmt.Errorf("GetStateChan requires a callback registry")
	}

	registrar, ok := fsm.callbacks.(HookRegistrar)
	if !ok {
		return fmt.Errorf("GetStateChan requires a callback registry that supports dynamic hook registration")
	}

	fsm.stateChanSetup.Do(func() {
		fsm.broadcastManager = broadcast.NewManager(fsm.logger.Handler())

		fsm.stateChanSetupErr = registrar.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
			Name:   "fsm.GetStateChan",
			From:   []string{"*"},
			To:     []string{"*"},
			Action: fsm.broadcastManager.BroadcastHook,
		})
	})

	if fsm.stateChanSetupErr != nil {
		return fsm.stateChanSetupErr
	}

	_, err := fsm.broadcastManager.GetStateChan(
		ctx,
		broadcast.WithCustomChannel(c),
		broadcast.WithTimeout(fsm.broadcastTimeout),
	)
	if err != nil {
		return fmt.Errorf("failed to register channel: %w", err)
	}

	c <- fsm.GetState()

	return nil
}
