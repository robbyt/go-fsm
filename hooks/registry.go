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
)

// transitionDB provides state lookup and enumeration.
// This interface is satisfied by transitions.Config.
type transitionDB interface {
	HasState(state string) bool
	GetAllStates() []string
}

// GuardFunc is the signature for guards that can abort transitions.
// Returns an error to reject the transition.
type GuardFunc func(ctx context.Context, from, to string) error

// ActionFunc is the signature for actions that cannot abort transitions.
// These execute after the point of no return and are used for side effects only.
type ActionFunc func(ctx context.Context, from, to string)

// transitionKey uniquely identifies a transition from one state to another.
type transitionKey struct {
	from string
	to   string
}

// hookEntry stores a registered hook with its function and metadata.
type hookEntry struct {
	guard      GuardFunc
	action     ActionFunc
	fromStates []string
	toStates   []string
	hookType   HookType
}

// Registry executes hooks synchronously in FIFO order.
// It handles panic recovery and error wrapping for all hook executions.
type Registry struct {
	mu                       sync.RWMutex
	logger                   *slog.Logger
	transitions              transitionDB
	preTransitionHooksIndex  map[transitionKey][]*hookEntry
	postTransitionHooksIndex map[transitionKey][]*hookEntry
	hooks                    map[string]*hookEntry
}

// NewRegistry creates a new synchronous hook registry.
func NewRegistry(opts ...Option) (*Registry, error) {
	r := &Registry{
		logger:                   slog.Default(),
		preTransitionHooksIndex:  make(map[transitionKey][]*hookEntry),
		postTransitionHooksIndex: make(map[transitionKey][]*hookEntry),
		hooks:                    make(map[string]*hookEntry),
	}

	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	return r, nil
}

// ExecutePreTransitionHooks runs all registered pre-transition hooks for the specific transition in FIFO order.
// Returns an error if any pre-transition hook fails.
func (r *Registry) ExecutePreTransitionHooks(ctx context.Context, from, to string) error {
	r.mu.RLock()
	entries := r.preTransitionHooksIndex[transitionKey{from, to}]
	r.mu.RUnlock()

	for i, entry := range entries {
		if err := r.safeCallGuard(ctx, entry.guard, from, to); err != nil {
			return fmt.Errorf("%w during transition at index %d: %w",
				ErrCallbackFailed, i, err)
		}
	}
	return nil
}

// ExecutePostTransitionHooks runs all registered post-transition hooks in FIFO order.
// Panics are recovered and logged but do not propagate.
func (r *Registry) ExecutePostTransitionHooks(ctx context.Context, from, to string) {
	r.mu.RLock()
	entries := r.postTransitionHooksIndex[transitionKey{from, to}]
	r.mu.RUnlock()

	for _, entry := range entries {
		r.safeCallAction(ctx, entry.action, from, to)
	}
}

// GetHooks returns information about all registered hooks.
func (r *Registry) GetHooks() []HookInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hooks := make([]HookInfo, 0, len(r.hooks))
	for name, entry := range r.hooks {
		hooks = append(hooks, HookInfo{
			Name:       name,
			FromStates: entry.fromStates,
			ToStates:   entry.toStates,
			Type:       entry.hookType,
		})
	}

	return hooks
}

// RegisterPreTransitionHook registers a pre-transition hook for transitions matching the patterns.
// Accepts a configuration struct with name, state patterns (use "*" for any state), and guard.
// Resolves patterns and registers the guard for each concrete (from, to) transition.
func (r *Registry) RegisterPreTransitionHook(config PreTransitionHookConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := &hookEntry{
		guard:      config.Guard,
		fromStates: config.From,
		toStates:   config.To,
		hookType:   HookTypePre,
	}

	keys, err := r.registerHookEntry(config.Name, entry)
	if err != nil {
		return err
	}

	for _, key := range keys {
		r.preTransitionHooksIndex[key] = append(r.preTransitionHooksIndex[key], entry)
	}

	return nil
}

// RegisterPostTransitionHook registers a post-transition hook for transitions matching the patterns.
// Accepts a configuration struct with name, state patterns (use "*" for any state), and action.
// Resolves patterns and registers the action for each concrete (from, to) transition.
func (r *Registry) RegisterPostTransitionHook(config PostTransitionHookConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := &hookEntry{
		action:     config.Action,
		fromStates: config.From,
		toStates:   config.To,
		hookType:   HookTypePost,
	}

	keys, err := r.registerHookEntry(config.Name, entry)
	if err != nil {
		return err
	}

	for _, key := range keys {
		r.postTransitionHooksIndex[key] = append(r.postTransitionHooksIndex[key], entry)
	}

	return nil
}

// RemoveHook removes a hook by name from all registrations.
func (r *Registry) RemoveHook(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.hooks[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrHookNotFound, name)
	}

	// Linear scan: remove the entry pointer from all transitions
	if entry.hookType == HookTypePre {
		for key := range r.preTransitionHooksIndex {
			r.preTransitionHooksIndex[key] = removePointerFromSlice(r.preTransitionHooksIndex[key], entry)
		}
	} else {
		for key := range r.postTransitionHooksIndex {
			r.postTransitionHooksIndex[key] = removePointerFromSlice(r.postTransitionHooksIndex[key], entry)
		}
	}

	delete(r.hooks, name)

	return nil
}

// Clear removes all registered hooks.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.preTransitionHooksIndex = make(map[transitionKey][]*hookEntry)
	r.postTransitionHooksIndex = make(map[transitionKey][]*hookEntry)
	r.hooks = make(map[string]*hookEntry)
}

// registerHookEntry performs common validation and stores a hookEntry for registration.
// Must be called while holding r.mu lock.
func (r *Registry) registerHookEntry(name string, entry *hookEntry) ([]transitionKey, error) {
	if name == "" {
		return nil, ErrHookNameEmpty
	}

	if len(entry.fromStates) == 0 || len(entry.toStates) == 0 {
		return nil, fmt.Errorf("from and to state lists cannot be empty")
	}

	if _, exists := r.hooks[name]; exists {
		return nil, fmt.Errorf("%w: %s", ErrHookNameAlreadyExists, name)
	}

	keys, err := r.buildIndexKeys(entry.fromStates, entry.toStates)
	if err != nil {
		return nil, err
	}

	r.hooks[name] = entry

	return keys, nil
}

// buildIndexKeys builds index keys from pattern strings by expanding wildcards and computing the cartesian product.
// TODO: what if the GetAllStates() returns data that becomes stale later?
func (r *Registry) buildIndexKeys(fromPatterns, toPatterns []string) ([]transitionKey, error) {
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

// removePointerFromSlice removes a hookEntry pointer from a slice and returns the new slice.
func removePointerFromSlice(slice []*hookEntry, target *hookEntry) []*hookEntry {
	result := make([]*hookEntry, 0, len(slice))
	for _, entry := range slice {
		if entry != target {
			result = append(result, entry)
		}
	}
	return result
}

// safeCallGuard executes a guard with panic recovery.
// If the guard panics, the panic is recovered and returned as an error.
func (r *Registry) safeCallGuard(ctx context.Context, guard GuardFunc, from, to string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("Guard panicked",
				"panic", rec, "from", from, "to", to)
			err = fmt.Errorf("%w: %v", ErrCallbackPanic, rec)
		}
	}()

	return guard(ctx, from, to)
}

// safeCallAction executes an action with panic recovery.
// Panics are logged but do not propagate.
func (r *Registry) safeCallAction(ctx context.Context, action ActionFunc, from, to string) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("Action panicked",
				"panic", rec, "from", from, "to", to)
		}
	}()

	action(ctx, from, to)
}
