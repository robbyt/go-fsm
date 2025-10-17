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

// hookMetadata stores the original registration information.
type hookMetadata struct {
	fromStates []string
	toStates   []string
	hookType   HookType
}

// Registry executes hooks synchronously in FIFO order.
// It handles panic recovery and error wrapping for all hook executions.
type Registry struct {
	mu                  sync.RWMutex
	logger              *slog.Logger
	transitions         transitionDB
	preTransitionHooks  map[transitionKey][]string
	postTransitionHooks map[transitionKey][]string
	preHooks            map[string]GuardFunc
	postHooks           map[string]ActionFunc
	metadata            map[string]hookMetadata
}

// NewRegistry creates a new synchronous hook registry.
func NewRegistry(opts ...Option) (*Registry, error) {
	r := &Registry{
		logger:              slog.Default(),
		preTransitionHooks:  make(map[transitionKey][]string),
		postTransitionHooks: make(map[transitionKey][]string),
		preHooks:            make(map[string]GuardFunc),
		postHooks:           make(map[string]ActionFunc),
		metadata:            make(map[string]hookMetadata),
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
func (r *Registry) ExecutePreTransitionHooks(ctx context.Context, from, to string) error {
	r.mu.RLock()
	hookNames := r.preTransitionHooks[transitionKey{from, to}]
	r.mu.RUnlock()

	for i, name := range hookNames {
		guard := r.getPreHook(name)
		if err := r.safeCallGuard(ctx, guard, from, to); err != nil {
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
	hookNames := r.postTransitionHooks[transitionKey{from, to}]
	r.mu.RUnlock()

	for _, name := range hookNames {
		action := r.getPostHook(name)
		r.safeCallAction(ctx, action, from, to)
	}
}

// getPreHook retrieves a pre-transition hook guard by name.
func (r *Registry) getPreHook(name string) GuardFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.preHooks[name]
}

// getPostHook retrieves a post-transition hook action by name.
func (r *Registry) getPostHook(name string) ActionFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.postHooks[name]
}

// registerHookCommon performs common validation and metadata storage for hook registration.
// Must be called while holding r.mu lock.
func (r *Registry) registerHookCommon(name string, from, to []string, hookType HookType) ([]transitionKey, error) {
	if name == "" {
		return nil, ErrHookNameEmpty
	}

	if len(from) == 0 || len(to) == 0 {
		return nil, fmt.Errorf("from and to state lists cannot be empty")
	}

	if _, exists := r.metadata[name]; exists {
		return nil, fmt.Errorf("%w: %s", ErrHookNameAlreadyExists, name)
	}

	keys, err := r.resolveTransitionPatterns(from, to)
	if err != nil {
		return nil, err
	}

	r.metadata[name] = hookMetadata{
		fromStates: from,
		toStates:   to,
		hookType:   hookType,
	}

	return keys, nil
}

// RegisterPreTransitionHook registers a pre-transition hook for transitions matching the patterns.
// Accepts a configuration struct with name, state patterns (use "*" for any state), and guard.
// Resolves patterns and registers the guard for each concrete (from, to) transition.
func (r *Registry) RegisterPreTransitionHook(config PreTransitionHookConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	keys, err := r.registerHookCommon(config.Name, config.From, config.To, HookTypePre)
	if err != nil {
		return err
	}

	r.preHooks[config.Name] = config.Guard
	for _, key := range keys {
		r.preTransitionHooks[key] = append(r.preTransitionHooks[key], config.Name)
	}

	return nil
}

// RegisterPostTransitionHook registers a post-transition hook for transitions matching the patterns.
// Accepts a configuration struct with name, state patterns (use "*" for any state), and action.
// Resolves patterns and registers the action for each concrete (from, to) transition.
func (r *Registry) RegisterPostTransitionHook(config PostTransitionHookConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	keys, err := r.registerHookCommon(config.Name, config.From, config.To, HookTypePost)
	if err != nil {
		return err
	}

	r.postHooks[config.Name] = config.Action
	for _, key := range keys {
		r.postTransitionHooks[key] = append(r.postTransitionHooks[key], config.Name)
	}

	return nil
}

// GetHooks returns information about all registered hooks.
func (r *Registry) GetHooks() []HookInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hooks := make([]HookInfo, 0, len(r.metadata))
	for name, meta := range r.metadata {
		hooks = append(hooks, HookInfo{
			Name:       name,
			FromStates: meta.fromStates,
			ToStates:   meta.toStates,
			Type:       meta.hookType,
		})
	}

	return hooks
}

// RemoveHook removes a hook by name from all registrations.
func (r *Registry) RemoveHook(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta, exists := r.metadata[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrHookNotFound, name)
	}

	keys, err := r.resolveTransitionPatterns(meta.fromStates, meta.toStates)
	if err != nil {
		return err
	}

	for _, key := range keys {
		if meta.hookType == HookTypePre {
			r.preTransitionHooks[key] = removeFromSlice(r.preTransitionHooks[key], name)
		} else {
			r.postTransitionHooks[key] = removeFromSlice(r.postTransitionHooks[key], name)
		}
	}

	delete(r.metadata, name)
	if meta.hookType == HookTypePre {
		delete(r.preHooks, name)
	} else {
		delete(r.postHooks, name)
	}

	return nil
}

// removeFromSlice removes a string from a slice and returns the new slice.
func removeFromSlice(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// Clear removes all registered hooks.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.preTransitionHooks = make(map[transitionKey][]string)
	r.postTransitionHooks = make(map[transitionKey][]string)
	r.preHooks = make(map[string]GuardFunc)
	r.postHooks = make(map[string]ActionFunc)
	r.metadata = make(map[string]hookMetadata)
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
