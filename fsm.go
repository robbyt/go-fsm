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
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/robbyt/go-fsm/v2/hooks"
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
	mutex       sync.RWMutex
	state       atomic.Value
	transitions transitionDB
	logger      *slog.Logger
	callbacks   CallbackExecutor
}

// persistentState is used for JSON marshaling/unmarshaling.
type persistentState struct {
	State       string              `json:"state"`
	Transitions map[string][]string `json:"transitions"`
	Hooks       []hooks.HookInfo    `json:"hooks,omitempty"`
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
		transitions: trans,
		logger:      slog.New(handler),
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

// MarshalJSON implements the json.Marshaler interface.
// It serializes the current state, all transitions, and registered hooks.
func (fsm *Machine) MarshalJSON() ([]byte, error) {
	currentState := fsm.GetState()

	// Get transitions as a map
	var transitionsMap map[string][]string
	if transConfig, ok := fsm.transitions.(*transitions.Config); ok {
		transitionsMap = transConfig.AsMap()
	} else {
		return nil, fmt.Errorf("transitions is not *transitions.Config, cannot marshal")
	}

	pState := persistentState{
		State:       currentState,
		Transitions: transitionsMap,
	}

	// Get hooks if a registry is configured
	if fsm.callbacks != nil {
		type hookGetter interface {
			GetHooks() iter.Seq[hooks.HookInfo]
		}
		if registry, ok := fsm.callbacks.(hookGetter); ok {
			var hookList []hooks.HookInfo
			for hookInfo := range registry.GetHooks() {
				hookList = append(hookList, hookInfo)
			}
			pState.Hooks = hookList
		}
	}

	jsonData, err := json.Marshal(pState)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FSM state: %w", err)
	}

	return jsonData, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// It restores the state and transitions from JSON.
// Hooks are not restored and must be re-registered after unmarshaling.
func (fsm *Machine) UnmarshalJSON(data []byte) error {
	var pState persistentState
	if err := json.Unmarshal(data, &pState); err != nil {
		return fmt.Errorf("failed to unmarshal FSM: %w", err)
	}

	if pState.State == "" {
		return ErrEmptyState
	}

	if len(pState.Transitions) == 0 {
		return ErrEmptyTransitions
	}

	// Create transitions.Config from the map
	transConfig, err := transitions.New(pState.Transitions)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidJSONTransitions, err)
	}

	// Validate that the state exists in transitions
	if !transConfig.HasState(pState.State) {
		return fmt.Errorf("%w: state '%s' is not defined in transitions", ErrInvalidConfiguration, pState.State)
	}

	// Set the FSM fields
	fsm.state.Store(pState.State)
	fsm.transitions = transConfig
	fsm.callbacks = nil

	// Initialize logger if not already set
	if fsm.logger == nil {
		handler := slog.Default().Handler().WithGroup("fsm")
		fsm.logger = slog.New(handler)
	}

	// Warn if hooks were present in JSON
	if len(pState.Hooks) > 0 {
		fsm.logger.Warn("hooks from JSON will be dropped and must be re-registered",
			"hook_count", len(pState.Hooks))
	}

	return nil
}

// GetState returns the current state of the finite state machine.
func (fsm *Machine) GetState() string {
	return fsm.state.Load().(string)
}

// GetAllStates returns all allowed states that have been added to this FSM.
func (fsm *Machine) GetAllStates() []string {
	return fsm.transitions.GetAllStates()
}

// setState updates the FSM's state atomically.
// Assumes the caller holds the write lock.
func (fsm *Machine) setState(state string) {
	fsm.state.Store(state)
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

// Transition changes the FSM's state to toState if the transition is allowed.
// Returns an error if the transition is not permitted.
func (fsm *Machine) Transition(toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(context.Background(), toState)
}

// TransitionWithContext changes the FSM's state to toState with a context.
// The context is passed to all hooks for request-scoped values, tracing, or cancellation.
// Returns an error if the transition is not permitted.
func (fsm *Machine) TransitionWithContext(ctx context.Context, toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(ctx, toState)
}

// TransitionBool returns true if the transition to toState succeeds, false otherwise.
func (fsm *Machine) TransitionBool(toState string) bool {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(context.Background(), toState) == nil
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
	// Check if context is already canceled before starting transition
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("transition canceled: %w", err)
	}

	currentState := fsm.GetState()

	if !fsm.transitions.IsTransitionAllowed(currentState, toState) {
		return fmt.Errorf(
			"%w: transition from '%s' to '%s' is not allowed",
			ErrInvalidStateTransition,
			currentState,
			toState,
		)
	}

	if fsm.callbacks != nil {
		if err := fsm.callbacks.ExecutePreTransitionHooks(ctx, currentState, toState); err != nil {
			return err
		}
	}

	fsm.setState(toState)
	fsm.logger.Debug("Transition successful", "from", currentState, "to", toState)

	if fsm.callbacks != nil {
		fsm.callbacks.ExecutePostTransitionHooks(ctx, currentState, toState)
	}

	return nil
}
