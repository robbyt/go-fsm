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
	"cmp"
	"context"
	"fmt"
	"iter"
	"log/slog"
	"slices"
	"sync"
)

const WildcardStatePattern = "*"

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

// String returns a string representation of the transition key for debugging.
func (k transitionKey) String() string {
	return k.from + "->" + k.to
}

// hookEntry stores a registered hook with its function and metadata.
type hookEntry struct {
	guard           GuardFunc
	action          ActionFunc
	fromStates      []string
	toStates        []string
	hookType        HookType
	registrationSeq int
	wildcardPresent bool
}

// removeFrom removes this hookEntry from the given slice and returns the new slice.
func (e *hookEntry) removeFrom(slice []*hookEntry) []*hookEntry {
	return slices.DeleteFunc(slice, func(entry *hookEntry) bool {
		return entry == e
	})
}

// Registry executes hooks synchronously in FIFO order.
// It handles panic recovery and error wrapping for all hook executions.
//
// Hook storage strategy:
// - Concrete pattern hooks are indexed in maps for O(1) lookup by transition.
// - Wildcard pattern hooks are stored in slices and evaluated at runtime for dynamic state matching.
// - FIFO ordering is maintained across both types using registration sequence numbers.
type Registry struct {
	mu                       sync.RWMutex
	logger                   *slog.Logger
	transitions              transitionDB
	preTransitionHooksIndex  map[transitionKey][]*hookEntry
	postTransitionHooksIndex map[transitionKey][]*hookEntry
	preWildcardHooks         []*hookEntry
	postWildcardHooks        []*hookEntry
	hooks                    map[string]*hookEntry
	nextSeq                  int
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
	concreteEntries := r.preTransitionHooksIndex[transitionKey{from, to}]
	wildcardHooks := r.preWildcardHooks
	r.mu.RUnlock()

	// Fast path: no wildcard hooks registered, execute concrete hooks directly
	if len(wildcardHooks) == 0 {
		for i, entry := range concreteEntries {
			if err := r.safeCallGuard(ctx, entry.guard, from, to); err != nil {
				return fmt.Errorf("%w during transition at index %d: %w",
					ErrCallbackFailed, i, err)
			}
		}
		return nil
	}

	// Slow path: collect concrete + matching wildcard hooks, then sort
	allEntries := make([]*hookEntry, 0, len(concreteEntries)+len(wildcardHooks))
	allEntries = append(allEntries, concreteEntries...)
	for _, entry := range wildcardHooks {
		if matchesPattern(entry.fromStates, from) && matchesPattern(entry.toStates, to) {
			allEntries = append(allEntries, entry)
		}
	}

	// Sort by registration sequence to maintain FIFO order
	slices.SortFunc(allEntries, func(a, b *hookEntry) int {
		return cmp.Compare(a.registrationSeq, b.registrationSeq)
	})

	// Execute in order
	for i, entry := range allEntries {
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
	concreteEntries := r.postTransitionHooksIndex[transitionKey{from, to}]
	wildcardHooks := r.postWildcardHooks
	r.mu.RUnlock()

	// Fast path: no wildcard hooks registered, execute concrete hooks directly
	if len(wildcardHooks) == 0 {
		for _, entry := range concreteEntries {
			r.safeCallAction(ctx, entry.action, from, to)
		}
		return
	}

	// Slow path: collect concrete + matching wildcard hooks, then sort
	allEntries := make([]*hookEntry, 0, len(concreteEntries)+len(wildcardHooks))
	allEntries = append(allEntries, concreteEntries...)
	for _, entry := range wildcardHooks {
		if matchesPattern(entry.fromStates, from) && matchesPattern(entry.toStates, to) {
			allEntries = append(allEntries, entry)
		}
	}

	// Sort by registration sequence to maintain FIFO order
	slices.SortFunc(allEntries, func(a, b *hookEntry) int {
		return cmp.Compare(a.registrationSeq, b.registrationSeq)
	})

	// Execute in order
	for _, entry := range allEntries {
		r.safeCallAction(ctx, entry.action, from, to)
	}
}

// GetHooks returns an iterator over all registered hooks.
// The RLock is held during the entire iteration, so callers should consume the iterator promptly.
// The returned slices (FromStates, ToStates) are defensive copies.
func (r *Registry) GetHooks() iter.Seq[HookInfo] {
	return func(yield func(HookInfo) bool) {
		r.mu.RLock()
		defer r.mu.RUnlock()

		for name, entry := range r.hooks {
			if !yield(HookInfo{
				Name:       name,
				FromStates: slices.Clone(entry.fromStates),
				ToStates:   slices.Clone(entry.toStates),
				Type:       entry.hookType,
			}) {
				return
			}
		}
	}
}

// RegisterPreTransitionHook registers a pre-transition hook for transitions matching the patterns.
// Accepts a configuration struct with name, state patterns (use "*" for any state), and guard.
// Concrete patterns are indexed at registration; wildcard patterns are evaluated at runtime.
func (r *Registry) RegisterPreTransitionHook(config PreTransitionHookConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := &hookEntry{
		guard:      config.Guard,
		fromStates: config.From,
		toStates:   config.To,
		hookType:   HookTypePre,
	}

	keys, hasWildcard, err := r.registerHookEntry(config.Name, entry)
	if err != nil {
		return err
	}

	if hasWildcard {
		r.preWildcardHooks = append(r.preWildcardHooks, entry)
	} else {
		for _, key := range keys {
			r.preTransitionHooksIndex[key] = append(r.preTransitionHooksIndex[key], entry)
		}
	}

	return nil
}

// RegisterPostTransitionHook registers a post-transition hook for transitions matching the patterns.
// Accepts a configuration struct with name, state patterns (use "*" for any state), and action.
// Concrete patterns are indexed at registration; wildcard patterns are evaluated at runtime.
func (r *Registry) RegisterPostTransitionHook(config PostTransitionHookConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := &hookEntry{
		action:     config.Action,
		fromStates: config.From,
		toStates:   config.To,
		hookType:   HookTypePost,
	}

	keys, hasWildcard, err := r.registerHookEntry(config.Name, entry)
	if err != nil {
		return err
	}

	if hasWildcard {
		r.postWildcardHooks = append(r.postWildcardHooks, entry)
	} else {
		for _, key := range keys {
			r.postTransitionHooksIndex[key] = append(r.postTransitionHooksIndex[key], entry)
		}
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

	// Linear scan: remove the entry pointer from all transitions and wildcard slices
	if entry.hookType == HookTypePre {
		for key := range r.preTransitionHooksIndex {
			r.preTransitionHooksIndex[key] = entry.removeFrom(r.preTransitionHooksIndex[key])
		}
		r.preWildcardHooks = entry.removeFrom(r.preWildcardHooks)
	} else {
		for key := range r.postTransitionHooksIndex {
			r.postTransitionHooksIndex[key] = entry.removeFrom(r.postTransitionHooksIndex[key])
		}
		r.postWildcardHooks = entry.removeFrom(r.postWildcardHooks)
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
	r.preWildcardHooks = nil
	r.postWildcardHooks = nil
	r.hooks = make(map[string]*hookEntry)
	r.nextSeq = 0
}

// registerHookEntry performs common validation and stores a hookEntry for registration.
// Must be called while holding r.mu lock.
// Returns index keys for concrete patterns, or hasWildcard=true for wildcard patterns.
func (r *Registry) registerHookEntry(name string, entry *hookEntry) ([]transitionKey, bool, error) {
	if name == "" {
		return nil, false, ErrHookNameEmpty
	}

	if len(entry.fromStates) == 0 || len(entry.toStates) == 0 {
		return nil, false, fmt.Errorf("from and to state lists cannot be empty")
	}

	if _, exists := r.hooks[name]; exists {
		return nil, false, fmt.Errorf("%w: %s", ErrHookNameAlreadyExists, name)
	}

	// Assign registration sequence
	entry.registrationSeq = r.nextSeq
	r.nextSeq++

	// Check for wildcard patterns
	if hasWildcardPattern(entry.fromStates) || hasWildcardPattern(entry.toStates) {
		// Wildcard hooks require a transition table
		if r.transitions == nil {
			return nil, false, fmt.Errorf("wildcard '*' cannot be used without state table (use WithTransitions option)")
		}
		entry.wildcardPresent = true
		r.hooks[name] = entry
		return nil, true, nil
	}

	// Concrete patterns: build index keys
	entry.wildcardPresent = false
	keys, err := r.buildConcreteIndexKeys(entry.fromStates, entry.toStates)
	if err != nil {
		return nil, false, err
	}

	r.hooks[name] = entry
	return keys, false, nil
}

// hasWildcardPattern checks if any pattern in the list is a wildcard ("*").
func hasWildcardPattern(patterns []string) bool {
	return slices.Contains(patterns, WildcardStatePattern)
}

// buildConcreteIndexKeys builds index keys from concrete pattern strings.
// Assumes no wildcards are present in the patterns (caller must check first).
// Validates that all state names exist in the transition table if provided.
func (r *Registry) buildConcreteIndexKeys(fromPatterns, toPatterns []string) ([]transitionKey, error) {
	// Validate and collect from states
	var fromStates []string
	for _, pattern := range fromPatterns {
		if r.transitions != nil && !r.transitions.HasState(pattern) {
			return nil, fmt.Errorf("unknown state '%s'", pattern)
		}
		fromStates = append(fromStates, pattern)
	}

	// Validate and collect to states
	var toStates []string
	for _, pattern := range toPatterns {
		if r.transitions != nil && !r.transitions.HasState(pattern) {
			return nil, fmt.Errorf("unknown state '%s'", pattern)
		}
		toStates = append(toStates, pattern)
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

// matchesPattern checks if a state matches any pattern in the list.
// Returns true if patterns contains "*" (wildcard) or the specific state.
func matchesPattern(patterns []string, state string) bool {
	return slices.Contains(patterns, WildcardStatePattern) || slices.Contains(patterns, state)
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
