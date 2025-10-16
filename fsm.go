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
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/robbyt/go-fsm/v2/transitions"
)

type transitionDB interface {
	IsTransitionAllowed(from, to string) bool
	HasState(state string) bool
	GetAllStates() []string
}

// Machine represents a finite state machine that tracks its current state
// and manages state transitions.
type Machine struct {
	// mutex protects the 'state' field during transitions and SetState.
	// RLock is used for GetState if not using atomic.Value.
	mutex sync.RWMutex
	// state stores the current state. Using atomic.Value allows lock-free reads via GetState.
	// If simplified, replace with `state string` and use mutex.RLock in GetState.
	state atomic.Value
	// transitions provides fast lookups for allowed transitions.
	transitions transitionDB
	// logHandler is the underlying structured logging handler.
	logHandler slog.Handler
	// logger is the FSM's specific logger instance.
	logger *slog.Logger
	// callbacks holds the callback executor implementation.
	callbacks CallbackExecutor
}

// persistentState is used for JSON marshaling/unmarshaling.
// It only stores the essential state information.
// TODO: add ALL states and transitions to the JSON representation
type persistentState struct {
	State string `json:"state"`
}

// New initializes a new finite state machine with the specified initial state and
// allowed state transitions. This is the advanced constructor that accepts a transitionDB interface.
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
	// Use default logger
	handler := slog.Default().
		Handler().
		WithGroup("fsm")

	// Validate that transitions are defined.
	if trans == nil {
		return nil, fmt.Errorf("%w: transitions is nil", ErrAvailableStateData)
	}

	// Validate that the initial state is actually defined in the transitions.
	if !trans.HasState(initialState) {
		return nil, fmt.Errorf(
			"%w: initial state '%s' is not defined in allowedTransitions",
			ErrInvalidState,
			initialState,
		)
	}

	// Create the machine instance
	m := &Machine{
		transitions: trans,
		logHandler:  handler,
		logger:      slog.New(handler),
	}
	m.state.Store(initialState)

	// Apply user options.
	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	return m, nil
}

// NewSimple creates a new FSM with a map-based transition configuration.
// This is a convenience wrapper around New() that handles transition validation.
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

// MarshalJSON implements the json.Marshaler interface.
// It returns the current state of the FSM as a JSON object: {"state": "CURRENT_STATE"}.
func (fsm *Machine) MarshalJSON() ([]byte, error) {
	// Get the current state safely.
	currentState := fsm.GetState()

	// Create the struct for marshaling.
	pState := persistentState{
		State: currentState,
	}

	// Marshal the persistence struct into JSON bytes.
	jsonData, err := json.Marshal(pState)
	if err != nil {
		// This should never happen
		return nil, fmt.Errorf("failed to marshal FSM state: %w", err)
	}

	return jsonData, nil
}

// GetState returns the current state of the finite state machine.
// This read is lock-free due to the use of atomic.Value.
func (fsm *Machine) GetState() string {
	return fsm.state.Load().(string)
}

// GetAllStates returns all allowed states that have been added to this FSM
func (fsm *Machine) GetAllStates() []string {
	return fsm.transitions.GetAllStates()
}

// setState updates the FSM's state atomically.
// It assumes that the caller has already acquired the necessary write lock (fsm.mutex).
// Broadcast to subscribers is handled by post-transition hooks.
func (fsm *Machine) setState(state string) {
	fsm.state.Store(state)
}

// SetState updates the FSM's state to the provided state, skipping the transition rules and pre-transition hooks.
// This will always succeed, assuming the requested state is actually a valid *from* state.
func (fsm *Machine) SetState(state string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	// Check if the target state is a known **from** state in our transitions.
	// This ensures the state is valid according to the machine's configuration.
	if !fsm.transitions.HasState(state) {
		return fmt.Errorf(
			"%w: state '%s' is not defined as a source state in transitions",
			ErrInvalidState,
			state,
		)
	}

	fromState := fsm.GetState()
	fsm.setState(state)
	fsm.logger.Debug("Set state", "from", fromState, "to", state)

	// Manually call post-transition hooks since we bypassed normal transition flow
	if fsm.callbacks != nil {
		fsm.callbacks.ExecutePostTransitionHooks(fromState, state)
	}

	return nil
}

// transition attempts to change the FSM's state from the current state to toState.
// It returns an error if the transition is invalid according to the configured rules.
// Assumes the caller holds the write lock (fsm.mutex).
// Executes callbacks in the following order:
// 1. Validate transition is allowed (can reject transition)
// 2. Pre-transition hooks (can reject transition)
// 3. State update (point of no return)
// 4. Post-transition hooks (cannot reject)
func (fsm *Machine) transition(toState string) error {
	currentState := fsm.GetState()

	// 1. Validate this transition is allowed by querying the transitionDB
	if !fsm.transitions.IsTransitionAllowed(currentState, toState) {
		return fmt.Errorf(
			"%w: transition from '%s' to '%s' is not allowed",
			ErrInvalidStateTransition,
			currentState,
			toState,
		)
	}

	// 2. Execute pre-transition hooks
	if fsm.callbacks != nil {
		if err := fsm.callbacks.ExecutePreTransitionHooks(currentState, toState); err != nil {
			return err
		}
	}

	// 3. UPDATE STATE (point of no return)
	fsm.setState(toState)
	fsm.logger.Debug("Transition successful", "from", currentState, "to", toState)

	// 4. Execute post-transition hooks
	if fsm.callbacks != nil {
		fsm.callbacks.ExecutePostTransitionHooks(currentState, toState)
	}

	return nil
}

// Transition changes the FSM's state to toState. It ensures that the transition adheres to the
// allowed transitions defined during initialization. Returns ErrInvalidState if the current state
// is somehow invalid or ErrInvalidStateTransition if the transition is not allowed.
func (fsm *Machine) Transition(toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(toState)
}

// TransitionBool is similar to Transition, but returns a boolean indicating whether the transition
// was successful. It suppresses the specific error reason.
func (fsm *Machine) TransitionBool(toState string) bool {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(toState) == nil
}

// TransitionIfCurrentState changes the FSM's state to toState only if the current state matches
// fromState. This returns an error if the current state does not match or if the transition is
// not allowed from fromState to toState.
func (fsm *Machine) TransitionIfCurrentState(fromState, toState string) error {
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

	return fsm.transition(toState)
}
