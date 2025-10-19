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

package fsm

import (
	"context"

	"github.com/robbyt/go-fsm/v2/hooks"
)

// CallbackExecutor defines the interface for executing state transition callbacks.
// Implementations handle iteration, panic recovery, and error wrapping internally.
// The interface is defined where it's consumed (fsm package) following Go best practices.
// Registration of callbacks happens on the concrete implementation before passing to the FSM.
type CallbackExecutor interface {
	// ExecutePreTransitionHooks runs all registered pre-transition hooks.
	// Returns an error if any hook fails.
	// The context is passed to all hooks, allowing access to request-scoped values.
	ExecutePreTransitionHooks(ctx context.Context, from, to string) error

	// ExecutePostTransitionHooks runs all registered post-transition hooks.
	// Panics are recovered and logged but do not propagate.
	// The context is passed to all hooks, allowing access to request-scoped values.
	ExecutePostTransitionHooks(ctx context.Context, from, to string)
}

// HookRegistrar extends CallbackExecutor with dynamic hook registration.
// This interface is used by GetStateChan to register broadcast hooks dynamically.
// The hooks.Registry type implements this interface.
type HookRegistrar interface {
	RegisterPostTransitionHook(config hooks.PostTransitionHookConfig) error
	RegisterPreTransitionHook(config hooks.PreTransitionHookConfig) error
}
