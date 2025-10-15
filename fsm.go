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
//	logger := slog.Default()
//	machine, err := fsm.New(logger.Handler(), fsm.StatusNew, fsm.TypicalTransitions)
//	if err != nil {
//	    logger.Error("Failed to create FSM", "error", err)
//	    return
//	}
//
//	err = machine.Transition(fsm.StatusRunning)
//	if err != nil {
//	    logger.Error("Transition failed", "error", err)
//	}
//
// // Persist state
// jsonData, err := json.Marshal(machine)
//
//	if err != nil {
//	    logger.Error("Failed to marshal FSM state", "error", err)
//	}
//
// // Restore state
// restoredMachine, err := fsm.NewFromJSON(logger.Handler(), jsonData, fsm.TypicalTransitions)
//
//	if err != nil {
//	    logger.Error("Failed to restore FSM from JSON", "error", err)
//	} else {
//
//	    logger.Info("Restored FSM state", "state", restoredMachine.GetState())
//	}
package fsm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/robbyt/go-fsm/hooks"
	"github.com/robbyt/go-fsm/hooks/broadcast"
)

// Machine represents a finite state machine that tracks its current state
// and manages state transitions.
type Machine struct {
	// mutex protects the 'state' field during transitions and SetState.
	// RLock is used for GetState if not using atomic.Value.
	mutex sync.RWMutex
	// state stores the current state. Using atomic.Value allows lock-free reads via GetState.
	// If simplified, replace with `state string` and use mutex.RLock in GetState.
	state atomic.Value
	// transitionIndex provides fast lookups for allowed transitions.
	transitionIndex transitionIndex
	// logHandler is the underlying structured logging handler.
	logHandler slog.Handler
	// logger is the FSM's specific logger instance.
	logger *slog.Logger
	// callbacks holds the callback executor implementation.
	callbacks CallbackExecutor
	// Broadcast manages state change notifications to subscribers.
	Broadcast *broadcast.Manager
}

// persistentState is used for JSON marshaling/unmarshaling.
// It only stores the essential state information.
// TODO: add ALL states and transitions to the JSON representation
type persistentState struct {
	State string `json:"state"`
}

// New initializes a new finite state machine with the specified initial state and
// allowed state transitions.
//
// Example of allowedTransitions:
//
//	allowedTransitions := TransitionsConfig{
//	    StatusNew:       {StatusBooting, StatusError},
//	    StatusBooting:   {StatusRunning, StatusError},
//	    StatusRunning:   {StatusReloading, StatusExited, StatusError},
//	    StatusReloading: {StatusRunning, StatusError},
//	    StatusError:     {StatusNew, StatusExited},
//	    StatusExited:    {StatusNew},
//	}
func New(
	handler slog.Handler,
	initialState string,
	allowedTransitions TransitionsConfig,
	opts ...Option,
) (*Machine, error) {
	// Ensure a valid logger handler is provided or use a default.
	if handler == nil {
		handler = slog.Default().
			Handler().
			WithGroup("fsm")
		// Fallback to the default slog handler if nil.
	}

	// Validate that transitions are defined.
	if len(allowedTransitions) == 0 {
		return nil, fmt.Errorf("%w: allowedTransitions is empty or nil", ErrAvailableStateData)
	}

	// Build the transition index for efficient lookups.
	idx := makeIndex(allowedTransitions)

	// Validate that the initial state is actually defined in the transitions.
	if _, ok := idx[initialState]; !ok {
		return nil, fmt.Errorf(
			"%w: initial state '%s' is not defined in allowedTransitions",
			ErrInvalidState,
			initialState,
		)
	}

	// Create the machine instance.
	m := &Machine{
		transitionIndex: idx,
		logHandler:      handler,
		logger:          slog.New(handler),
		callbacks:       hooks.NewSynchronousCallbackRegistry(slog.New(handler)),
	}
	// Atomically store the initial state.
	m.state.Store(initialState)

	// Initialize broadcast manager.
	m.Broadcast = broadcast.NewManager(slog.New(handler), m.GetState)

	// Apply user options.
	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	return m, nil
}

// NewFromJSON creates a new finite state machine by unmarshaling JSON data.
// It requires the logging handler and the original transitions configuration
// used when the state was marshaled.
func NewFromJSON(
	handler slog.Handler,
	jsonData []byte,
	allowedTransitions TransitionsConfig,
	opts ...Option,
) (*Machine, error) {
	// Unmarshal the JSON data into the temporary persistence struct.
	var pState persistentState
	if err := json.Unmarshal(jsonData, &pState); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FSM JSON data: %w", err)
	}

	// Validate the unmarshaled state. Use the first state in transitions as a fallback?
	// For now, let's require the state to be valid according to the provided transitions.
	// We use New which performs this validation internally.
	// Note: New validates if the state exists as a *source* state in the transitions.
	// If a state can only be a target state, New might fail.
	// Consider validating against all known states derived from allowedTransitions instead.

	// Create a new FSM instance. New will validate the state against the transitions.
	// We use the unmarshaled state as the initial state and apply the same options.
	machine, err := New(handler, pState.State, allowedTransitions, opts...)
	if err != nil {
		// Wrap the error from New to provide context about JSON restoration failure.
		return nil, fmt.Errorf(
			"failed to initialize FSM with restored state '%s': %w",
			pState.State,
			err,
		)
	}

	// The machine is already initialized with the correct state by New.
	machine.logger.Debug("Successfully restored FSM state from JSON", "state", pState.State)
	return machine, nil
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
	states := make([]string, 0, len(fsm.transitionIndex))
	for state := range fsm.transitionIndex {
		states = append(states, state)
	}
	return states
}

// setState updates the FSM's state atomically.
// It assumes that the caller has already acquired the necessary write lock (fsm.mutex).
// Broadcast to subscribers is handled by post-transition hooks.
func (fsm *Machine) setState(state string) {
	fsm.state.Store(state)
}

// SetState updates the FSM's state to the provided state, bypassing the usual transition rules.
// It only succeeds if the requested state is defined as a valid *source* state
// in the allowedTransitions configuration.
func (fsm *Machine) SetState(state string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	// Check if the target state is a known source state in our transition index.
	// This ensures the state is valid according to the machine's configuration.
	if _, ok := fsm.transitionIndex[state]; !ok {
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
// 1. Guards (can reject transition)
// 2. Validate transition is allowed (can reject transition)
// 3. Exit actions (can reject transition)
// 4. Transition actions (can reject transition)
// 5. State update (point of no return)
// 6. Entry actions (cannot reject)
// 7. Post-transition hooks (cannot reject)
func (fsm *Machine) transition(toState string) error {
	currentState := fsm.GetState()

	// 1. Execute guards
	if fsm.callbacks != nil {
		if err := fsm.callbacks.ExecuteGuards(currentState, toState); err != nil {
			return err
		}
	}

	// 2. Validate transition is allowed
	allowedTransitions, ok := fsm.transitionIndex[currentState]
	if !ok {
		return fmt.Errorf(
			"%w: current state '%s' has no defined transitions",
			ErrInvalidState,
			currentState,
		)
	}
	if _, exists := allowedTransitions[toState]; !exists {
		return fmt.Errorf(
			"%w: transition from '%s' to '%s' is not allowed",
			ErrInvalidStateTransition,
			currentState,
			toState,
		)
	}

	// 3. Execute exit actions
	if fsm.callbacks != nil {
		if err := fsm.callbacks.ExecuteExitActions(currentState, toState); err != nil {
			return err
		}

		// 4. Execute transition actions
		if err := fsm.callbacks.ExecuteTransitionActions(currentState, toState); err != nil {
			return err
		}
	}

	// 5. UPDATE STATE (point of no return)
	fsm.setState(toState)
	fsm.logger.Debug("Transition successful", "from", currentState, "to", toState)

	if fsm.callbacks != nil {
		// 6. Execute entry actions (cannot abort)
		fsm.callbacks.ExecuteEntryActions(currentState, toState)

		// 7. Execute post-transition hooks (includes broadcast)
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
		fsm.logger.Debug(
			"Conditional transition skipped: current state mismatch",
			"current", currentState,
			"expected", fromState,
			"target", toState)
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

// GetStateChan returns a channel that receives state change notifications.
// The current state is sent immediately. Delegates to Broadcast.GetStateChan.
func (fsm *Machine) GetStateChan(ctx context.Context) <-chan string {
	return fsm.Broadcast.GetStateChan(ctx)
}

// GetStateChanWithOptions returns a channel configured with functional options.
// Delegates to Broadcast.GetStateChanWithOptions.
func (fsm *Machine) GetStateChanWithOptions(
	ctx context.Context,
	opts ...broadcast.Option,
) <-chan string {
	return fsm.Broadcast.GetStateChanWithOptions(ctx, opts...)
}

// GetStateChanBuffer returns a channel with a configurable buffer size.
// Delegates to Broadcast.GetStateChanBuffer.
func (fsm *Machine) GetStateChanBuffer(ctx context.Context, chanBufferSize int) <-chan string {
	return fsm.Broadcast.GetStateChanBuffer(ctx, chanBufferSize)
}

// AddSubscriber adds a channel to receive state change broadcasts.
// Delegates to Broadcast.AddSubscriber.
//
// Deprecated: Use GetStateChanWithOptions with broadcast.WithCustomChannel instead.
func (fsm *Machine) AddSubscriber(ch chan string) func() {
	return fsm.Broadcast.AddSubscriber(ch)
}
