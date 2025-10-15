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

package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/robbyt/go-fsm/transitions"
)

// transitionKey uniquely identifies a transition from one state to another.
type transitionKey struct {
	from string
	to   string
}

// SynchronousCallbackRegistry executes callbacks synchronously in FIFO order.
// It handles panic recovery and error wrapping for all callback executions.
type SynchronousCallbackRegistry struct {
	mu                 sync.RWMutex
	logger             *slog.Logger
	transitions        *transitions.Config
	allStates          map[string]bool
	preTransitionHooks map[transitionKey][]CallbackFunc
	postTransition     map[transitionKey][]ActionFunc
}

// NewSynchronousCallbackRegistry creates a new synchronous callback registry.
func NewSynchronousCallbackRegistry(opts ...Option) (*SynchronousCallbackRegistry, error) {
	r := &SynchronousCallbackRegistry{
		logger:             slog.Default(),
		preTransitionHooks: make(map[transitionKey][]CallbackFunc),
		postTransition:     make(map[transitionKey][]ActionFunc),
		allStates:          make(map[string]bool),
	}

	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	return r, nil
}

// getAllStatesFromTransitions extracts all unique states from the transition configuration.
func (r *SynchronousCallbackRegistry) getAllStatesFromTransitions() map[string]bool {
	states := make(map[string]bool)
	if r.transitions != nil {
		for _, state := range r.transitions.GetAllStates() {
			states[state] = true
		}
	}
	return states
}

// expandStatePattern expands a state pattern (which may contain "*") into concrete states.
// Returns all states if pattern is "*", otherwise validates and returns the single state.
func (r *SynchronousCallbackRegistry) expandStatePattern(pattern string) ([]string, error) {
	if pattern == "*" {
		// Return all known states
		if len(r.allStates) == 0 {
			return nil, fmt.Errorf("wildcard '*' cannot be used without state table (use WithTransitions option)")
		}
		states := make([]string, 0, len(r.allStates))
		for state := range r.allStates {
			states = append(states, state)
		}
		return states, nil
	}

	// Single state - validate it exists if we have a state table
	if len(r.allStates) > 0 && !r.allStates[pattern] {
		return nil, fmt.Errorf("unknown state '%s'", pattern)
	}

	return []string{pattern}, nil
}

// expandTransitionPatterns expands from/to state patterns into concrete (from, to) pairs.
func (r *SynchronousCallbackRegistry) expandTransitionPatterns(fromPatterns, toPatterns []string) ([]transitionKey, error) {
	var fromStates []string
	for _, pattern := range fromPatterns {
		expanded, err := r.expandStatePattern(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid 'from' state pattern '%s': %w", pattern, err)
		}
		fromStates = append(fromStates, expanded...)
	}

	var toStates []string
	for _, pattern := range toPatterns {
		expanded, err := r.expandStatePattern(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid 'to' state pattern '%s': %w", pattern, err)
		}
		toStates = append(toStates, expanded...)
	}

	// Compute cartesian product
	keys := make([]transitionKey, 0, len(fromStates)*len(toStates))
	for _, from := range fromStates {
		for _, to := range toStates {
			keys = append(keys, transitionKey{from, to})
		}
	}

	return keys, nil
}

// ExecutePreTransitionHooks runs all registered pre-transition hooks for the specific transition in FIFO order.
// Returns an error if any pre-transition hook fails.
func (r *SynchronousCallbackRegistry) ExecutePreTransitionHooks(from, to string) error {
	r.mu.RLock()
	preTransitionHooks := r.preTransitionHooks[transitionKey{from, to}]
	r.mu.RUnlock()

	for i, cb := range preTransitionHooks {
		if err := r.safeCallCallback(cb, from, to); err != nil {
			return fmt.Errorf("%w during transition at index %d: %w",
				ErrCallbackFailed, i, err)
		}
	}
	return nil
}

// ExecutePostTransitionHooks runs all registered post-transition hooks in FIFO order.
// Panics are recovered and logged but do not propagate.
func (r *SynchronousCallbackRegistry) ExecutePostTransitionHooks(from, to string) {
	r.mu.RLock()
	postHooks := r.postTransition[transitionKey{from, to}]
	r.mu.RUnlock()

	for _, hook := range postHooks {
		r.safeCallAction(hook, from, to)
	}
}

// RegisterPreTransitionHook registers a pre-transition hook for transitions matching the patterns.
// Accepts slices of from and to state patterns (use "*" for any state).
// Expands patterns and registers the callback for each concrete (from, to) transition.
func (r *SynchronousCallbackRegistry) RegisterPreTransitionHook(from []string, to []string, callback CallbackFunc) error {
	if len(from) == 0 || len(to) == 0 {
		return fmt.Errorf("from and to state lists cannot be empty")
	}

	keys, err := r.expandTransitionPatterns(from, to)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, key := range keys {
		r.preTransitionHooks[key] = append(r.preTransitionHooks[key], callback)
	}

	return nil
}

// RegisterPostTransitionHook registers a post-transition hook for transitions matching the patterns.
// Accepts slices of from and to state patterns (use "*" for any state).
// Expands patterns and registers the action for each concrete (from, to) transition.
func (r *SynchronousCallbackRegistry) RegisterPostTransitionHook(from []string, to []string, action ActionFunc) error {
	if len(from) == 0 || len(to) == 0 {
		return fmt.Errorf("from and to state lists cannot be empty")
	}

	keys, err := r.expandTransitionPatterns(from, to)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, key := range keys {
		r.postTransition[key] = append(r.postTransition[key], action)
	}

	return nil
}

// Clear removes all registered callbacks.
func (r *SynchronousCallbackRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.preTransitionHooks = make(map[transitionKey][]CallbackFunc)
	r.postTransition = make(map[transitionKey][]ActionFunc)
}

// safeCallCallback executes a callback with panic recovery.
// If the callback panics, the panic is recovered and returned as an error.
func (r *SynchronousCallbackRegistry) safeCallCallback(cb CallbackFunc, from, to string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("Callback panicked",
				"panic", rec, "from", from, "to", to)
			err = fmt.Errorf("%w: %v", ErrCallbackPanic, rec)
		}
	}()

	ctx := context.Background()
	return cb(ctx, from, to)
}

// safeCallAction executes an action with panic recovery.
// Panics are logged but do not propagate.
func (r *SynchronousCallbackRegistry) safeCallAction(action ActionFunc, from, to string) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("Action panicked",
				"panic", rec, "from", from, "to", to)
		}
	}()

	ctx := context.Background()
	action(ctx, from, to)
}
