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
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// transitionDB provides state lookup and enumeration.
// This interface is satisfied by transitions.Config.
type transitionDB interface {
	HasState(state string) bool
	GetAllStates() []string
}

// CallbackFunc is the signature for callbacks that can abort transitions.
// Returns an error to reject the transition.
type CallbackFunc func(ctx context.Context, from, to string) error

// ActionFunc is the signature for callbacks that cannot abort transitions.
// These execute after the point of no return and are used for side effects only.
type ActionFunc func(ctx context.Context, from, to string)

var (
	// ErrGuardRejected is returned when a guard condition rejects a transition
	ErrGuardRejected = errors.New("guard rejected transition")

	// ErrCallbackFailed is returned when a callback fails during transition
	ErrCallbackFailed = errors.New("callback failed")

	// ErrCallbackPanic is returned when a callback panics
	ErrCallbackPanic = errors.New("callback panicked")
)

// transitionKey uniquely identifies a transition from one state to another.
type transitionKey struct {
	from string
	to   string
}

// Registry executes callbacks synchronously in FIFO order.
// It handles panic recovery and error wrapping for all callback executions.
type Registry struct {
	mu                 sync.RWMutex
	logger             *slog.Logger
	transitions        transitionDB
	preTransitionHooks map[transitionKey][]CallbackFunc
	postTransition     map[transitionKey][]ActionFunc
}

// NewRegistry creates a new synchronous callback registry.
func NewRegistry(opts ...Option) (*Registry, error) {
	r := &Registry{
		logger:             slog.Default(),
		preTransitionHooks: make(map[transitionKey][]CallbackFunc),
		postTransition:     make(map[transitionKey][]ActionFunc),
	}

	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	return r, nil
}

// resolveTransitionPatterns resolves from/to state patterns into concrete (from, to) pairs.
func (r *Registry) resolveTransitionPatterns(fromPatterns, toPatterns []string) ([]transitionKey, error) {
	var fromStates []string
	for _, pattern := range fromPatterns {
		if pattern == "*" {
			if r.transitions == nil {
				return nil, fmt.Errorf("wildcard '*' cannot be used without state table (use WithTransitions option)")
			}
			fromStates = append(fromStates, r.transitions.GetAllStates()...)
		} else {
			if r.transitions != nil && !r.transitions.HasState(pattern) {
				return nil, fmt.Errorf("unknown state '%s'", pattern)
			}
			fromStates = append(fromStates, pattern)
		}
	}

	var toStates []string
	for _, pattern := range toPatterns {
		if pattern == "*" {
			if r.transitions == nil {
				return nil, fmt.Errorf("wildcard '*' cannot be used without state table (use WithTransitions option)")
			}
			toStates = append(toStates, r.transitions.GetAllStates()...)
		} else {
			if r.transitions != nil && !r.transitions.HasState(pattern) {
				return nil, fmt.Errorf("unknown state '%s'", pattern)
			}
			toStates = append(toStates, pattern)
		}
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
func (r *Registry) ExecutePreTransitionHooks(from, to string) error {
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
func (r *Registry) ExecutePostTransitionHooks(from, to string) {
	r.mu.RLock()
	postHooks := r.postTransition[transitionKey{from, to}]
	r.mu.RUnlock()

	for _, hook := range postHooks {
		r.safeCallAction(hook, from, to)
	}
}

// RegisterPreTransitionHook registers a pre-transition hook for transitions matching the patterns.
// Accepts slices of from and to state patterns (use "*" for any state).
// Resolves patterns and registers the callback for each concrete (from, to) transition.
func (r *Registry) RegisterPreTransitionHook(from []string, to []string, callback CallbackFunc) error {
	if len(from) == 0 || len(to) == 0 {
		return fmt.Errorf("from and to state lists cannot be empty")
	}

	keys, err := r.resolveTransitionPatterns(from, to)
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
// Resolves patterns and registers the action for each concrete (from, to) transition.
func (r *Registry) RegisterPostTransitionHook(from []string, to []string, action ActionFunc) error {
	if len(from) == 0 || len(to) == 0 {
		return fmt.Errorf("from and to state lists cannot be empty")
	}

	keys, err := r.resolveTransitionPatterns(from, to)
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
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.preTransitionHooks = make(map[transitionKey][]CallbackFunc)
	r.postTransition = make(map[transitionKey][]ActionFunc)
}

// safeCallCallback executes a callback with panic recovery.
// If the callback panics, the panic is recovered and returned as an error.
func (r *Registry) safeCallCallback(cb CallbackFunc, from, to string) (err error) {
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
func (r *Registry) safeCallAction(action ActionFunc, from, to string) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("Action panicked",
				"panic", rec, "from", from, "to", to)
		}
	}()

	ctx := context.Background()
	action(ctx, from, to)
}
