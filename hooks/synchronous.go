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
	"regexp"
	"strings"
	"sync"
)

// transitionKey uniquely identifies a transition from one state to another.
type transitionKey struct {
	from string
	to   string
}

// SynchronousCallbackRegistry executes callbacks synchronously in FIFO order.
// It handles panic recovery and error wrapping for all callback executions.
type SynchronousCallbackRegistry struct {
	mu                sync.RWMutex
	logger            *slog.Logger
	guards            map[transitionKey][]CallbackFunc
	exitActions       map[string][]CallbackFunc
	transitionActions map[transitionKey][]CallbackFunc
	entryActions      map[string][]ActionFunc
	postTransition    []ActionFunc
}

// NewSynchronousCallbackRegistry creates a new synchronous callback registry.
func NewSynchronousCallbackRegistry(logger *slog.Logger) *SynchronousCallbackRegistry {
	return &SynchronousCallbackRegistry{
		logger:            logger,
		guards:            make(map[transitionKey][]CallbackFunc),
		exitActions:       make(map[string][]CallbackFunc),
		transitionActions: make(map[transitionKey][]CallbackFunc),
		entryActions:      make(map[string][]ActionFunc),
		postTransition:    make([]ActionFunc, 0),
	}
}

// ExecuteGuards runs all registered guards for the transition in FIFO order.
// Returns an error if any guard rejects the transition.
func (r *SynchronousCallbackRegistry) ExecuteGuards(from, to string) error {
	r.mu.RLock()
	guards := r.guards[transitionKey{from, to}]
	r.mu.RUnlock()

	for i, guard := range guards {
		if err := r.safeCallCallback(guard, from, to); err != nil {
			r.logger.Debug("Guard rejected transition",
				"from", from, "to", to, "guard_index", i, "error", err)
			return fmt.Errorf("%w at index %d: %w", ErrGuardRejected, i, err)
		}
	}
	return nil
}

// ExecuteExitActions runs all registered exit actions for the source state in FIFO order.
// Returns an error if any exit action fails.
func (r *SynchronousCallbackRegistry) ExecuteExitActions(from, to string) error {
	r.mu.RLock()
	exitActions := r.exitActions[from]
	r.mu.RUnlock()

	for i, cb := range exitActions {
		if err := r.safeCallCallback(cb, from, to); err != nil {
			return fmt.Errorf("%w during exit from '%s' at index %d: %w",
				ErrCallbackFailed, from, i, err)
		}
	}
	return nil
}

// ExecuteTransitionActions runs all registered transition actions for the specific transition in FIFO order.
// Returns an error if any transition action fails.
func (r *SynchronousCallbackRegistry) ExecuteTransitionActions(from, to string) error {
	r.mu.RLock()
	transitionActions := r.transitionActions[transitionKey{from, to}]
	r.mu.RUnlock()

	for i, cb := range transitionActions {
		if err := r.safeCallCallback(cb, from, to); err != nil {
			return fmt.Errorf("%w during transition at index %d: %w",
				ErrCallbackFailed, i, err)
		}
	}
	return nil
}

// ExecuteEntryActions runs all registered entry actions for the target state in FIFO order.
// Panics are recovered and logged but do not propagate.
func (r *SynchronousCallbackRegistry) ExecuteEntryActions(from, to string) {
	r.mu.RLock()
	entryActions := r.entryActions[to]
	r.mu.RUnlock()

	for _, action := range entryActions {
		r.safeCallAction(action, from, to)
	}
}

// ExecutePostTransitionHooks runs all registered post-transition hooks in FIFO order.
// Panics are recovered and logged but do not propagate.
func (r *SynchronousCallbackRegistry) ExecutePostTransitionHooks(from, to string) {
	r.mu.RLock()
	postHooks := r.postTransition
	r.mu.RUnlock()

	for _, hook := range postHooks {
		r.safeCallAction(hook, from, to)
	}
}

// RegisterGuard registers a guard function for a specific transition.
func (r *SynchronousCallbackRegistry) RegisterGuard(from, to string, guard CallbackFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := transitionKey{from, to}
	r.guards[key] = append(r.guards[key], guard)
}

// RegisterExitAction registers an exit action for a specific state.
func (r *SynchronousCallbackRegistry) RegisterExitAction(state string, callback CallbackFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.exitActions[state] = append(r.exitActions[state], callback)
}

// RegisterTransitionAction registers a transition action for a specific transition.
func (r *SynchronousCallbackRegistry) RegisterTransitionAction(from, to string, callback CallbackFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := transitionKey{from, to}
	r.transitionActions[key] = append(r.transitionActions[key], callback)
}

// RegisterEntryAction registers an entry action for a specific state.
func (r *SynchronousCallbackRegistry) RegisterEntryAction(state string, action ActionFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entryActions[state] = append(r.entryActions[state], action)
}

// RegisterPostTransitionHook registers a post-transition hook that runs after every transition.
func (r *SynchronousCallbackRegistry) RegisterPostTransitionHook(action ActionFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.postTransition = append(r.postTransition, action)
}

// Clear removes all registered callbacks.
func (r *SynchronousCallbackRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.guards = make(map[transitionKey][]CallbackFunc)
	r.exitActions = make(map[string][]CallbackFunc)
	r.transitionActions = make(map[transitionKey][]CallbackFunc)
	r.entryActions = make(map[string][]ActionFunc)
	r.postTransition = make([]ActionFunc, 0)
}

// safeCallCallback executes a callback with panic recovery.
// If the callback panics, the panic is recovered and returned as an error.
func (r *SynchronousCallbackRegistry) safeCallCallback(
	cb CallbackFunc,
	from, to string,
) (err error) {
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
func (r *SynchronousCallbackRegistry) safeCallAction(
	action ActionFunc,
	from, to string,
) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("Action panicked",
				"panic", rec, "from", from, "to", to)
		}
	}()

	ctx := context.Background()
	action(ctx, from, to)
}

// RegisterEntryActionPattern registers an entry action for states matching the pattern.
// The pattern can be an exact state name or a regex pattern (e.g., "Running", ".*ing$").
// Returns an error if the pattern is invalid or matches no states.
func (r *SynchronousCallbackRegistry) RegisterEntryActionPattern(
	pattern string,
	action ActionFunc,
	allStates []string,
) error {
	matchedStates := ResolveStatePattern(pattern, allStates)
	if len(matchedStates) == 0 {
		return fmt.Errorf("entry action pattern '%s' matches no states", pattern)
	}

	for _, state := range matchedStates {
		r.RegisterEntryAction(state, action)
	}

	return nil
}

// RegisterExitActionPattern registers an exit action for states matching the pattern.
// The pattern can be an exact state name or a regex pattern (e.g., "Running", ".*ing$").
// Returns an error if the pattern is invalid or matches no states.
func (r *SynchronousCallbackRegistry) RegisterExitActionPattern(
	pattern string,
	callback CallbackFunc,
	allStates []string,
) error {
	matchedStates := ResolveStatePattern(pattern, allStates)
	if len(matchedStates) == 0 {
		return fmt.Errorf("exit action pattern '%s' matches no states", pattern)
	}

	for _, state := range matchedStates {
		r.RegisterExitAction(state, callback)
	}

	return nil
}

// ResolveStatePattern resolves a state pattern to concrete state names.
// The pattern can be an exact state name or a regex pattern.
// Returns nil if the pattern is invalid or matches no states.
func ResolveStatePattern(pattern string, allStates []string) []string {
	if isRegexPattern(pattern) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil
		}

		var matched []string
		for _, state := range allStates {
			if re.MatchString(state) {
				matched = append(matched, state)
			}
		}
		return matched
	}

	// Exact match - verify state exists
	for _, state := range allStates {
		if state == pattern {
			return []string{pattern}
		}
	}

	return nil
}

// isRegexPattern returns true if the string contains regex meta-characters.
func isRegexPattern(s string) bool {
	return strings.ContainsAny(s, ".*+?[]{}()^$|\\")
}
