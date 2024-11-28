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
// and subscribing to state updates via channels.
//
// Example usage:
//
//	logger := slog.Default()
//	machine, err := fsm.New(logger, fsm.StatusNew, fsm.TypicalTransitions)
//	if err != nil {
//	    logger.Error("Failed to create FSM", "error", err)
//	    return
//	}
//
//	err = machine.Transition(fsm.StatusRunning)
//	if err != nil {
//	    logger.Error("Transition failed", "error", err)
//	}
package fsm

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// Machine represents a finite state machine that tracks its current state
// and manages state transitions.
type Machine struct {
	mutex              sync.RWMutex
	state              string
	allowedTransitions transitionConfigWithIndex
	subscriberMutex    sync.Mutex
	subscribers        map[chan string]struct{}
	channelBufferSize  int // used when creating the status channel, must be > 0
	logHandler         slog.Handler
	logger             *slog.Logger
}

// New initializes a new finite state machine with the specified initial state and
// allowed state transitions.
//
// Example of allowedTransitions:
//
//	allowedTransitions := TransitionsConfig{
//		StatusNew:       {StatusBooting, StatusError},
//		StatusBooting:   {StatusRunning, StatusError},
//		StatusRunning:   {StatusReloading, StatusExited, StatusError},
//		StatusReloading: {StatusRunning, StatusError},
//		StatusError:     {StatusNew, StatusExited},
//		StatusExited:    {StatusNew},
//	}
func New(handler slog.Handler, initialState string, allowedTransitions TransitionsConfig) (*Machine, error) {
	if handler == nil {
		defaultHandler := slog.NewTextHandler(os.Stdout, nil)
		handler = defaultHandler.WithGroup("fsm")
		slog.New(handler).Warn("Handler is nil, using the default logger configuration.")
	}

	if allowedTransitions == nil || len(allowedTransitions) == 0 {
		return nil, fmt.Errorf("%w: allowedTransitions is empty or nil", ErrAvailableStateData)
	}

	if _, ok := allowedTransitions[initialState]; !ok {
		return nil, fmt.Errorf("%w: initial state '%s' is not defined", ErrInvalidState, initialState)
	}

	trnsIndex := newTransitionWithIndex(allowedTransitions)

	return &Machine{
		state:              initialState,
		allowedTransitions: trnsIndex,
		subscribers:        make(map[chan string]struct{}),
		channelBufferSize:  defaultStateChanBufferSize,
		logHandler:         handler,
		logger:             slog.New(handler),
	}, nil
}

// GetState returns the current state of the finite state machine.
func (fsm *Machine) GetState() string {
	fsm.mutex.RLock()
	defer fsm.mutex.RUnlock()
	return fsm.state
}

// setState updates the FSM's state and notifies all subscribers about the state change.
// It assumes that the caller has already acquired the necessary locks.
func (fsm *Machine) setState(state string) {
	fsm.state = state
	fsm.broadcast(state)
}

// SetState updates the FSM's state to the provided state, bypassing the usual transition rules.
// It only succeeds if the requested state is defined in the allowedTransitions configuration.
func (fsm *Machine) SetState(state string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	if _, ok := fsm.allowedTransitions[state]; !ok {
		return fmt.Errorf("%w: state '%s' is not defined", ErrInvalidState, state)
	}

	fsm.setState(state)
	return nil
}

// transition attempts to change the FSM's state to toState. It returns an error if the transition
// is invalid or if the target state is not allowed from the current state.
func (fsm *Machine) transition(toState string) error {
	currentState := fsm.state
	allowedTransitions, ok := fsm.allowedTransitions[currentState]
	if !ok {
		return fmt.Errorf("%w: current state is '%s'", ErrInvalidState, currentState)
	}

	if _, exists := allowedTransitions[toState]; exists {
		fsm.setState(toState)
		fsm.logger.Debug("Transition successful", "from", currentState, "to", toState)
		return nil
	}

	return fmt.Errorf("%w: from '%s' to '%s'", ErrInvalidStateTransition, currentState, toState)
}

// Transition changes the FSM's state to toState. It ensures that the transition adheres to the
// allowed transitions. Returns ErrInvalidState if the target state is undefined, or
// ErrInvalidStateTransition if the transition is not allowed.
func (fsm *Machine) Transition(toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(toState)
}

// TransitionBool is similar to Transition, but returns a boolean indicating whether the transition
// was successful.
func (fsm *Machine) TransitionBool(toState string) bool {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()
	return fsm.transition(toState) == nil
}

// TransitionIfCurrentState changes the FSM's state to toState only if the current state matches
// fromState. This returns an error if the current state does not match or if the transition is
// not allowed.
func (fsm *Machine) TransitionIfCurrentState(fromState, toState string) error {
	fsm.mutex.Lock()
	defer fsm.mutex.Unlock()

	if fsm.state != fromState {
		return fmt.Errorf("%w: current state is '%s', expected '%s'", ErrCurrentStateIncorrect, fsm.state, fromState)
	}

	return fsm.transition(toState)
}
