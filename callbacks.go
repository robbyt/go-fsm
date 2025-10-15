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

// CallbackExecutor defines the interface for executing state transition callbacks.
// Implementations handle iteration, panic recovery, and error wrapping internally.
// The interface is defined where it's consumed (fsm package) following Go best practices.
// Registration of callbacks happens on the concrete implementation before passing to the FSM.
type CallbackExecutor interface {
	// ExecuteGuards runs all registered guards for the transition.
	// Returns an error if any guard rejects the transition.
	ExecuteGuards(from, to string) error

	// ExecuteExitActions runs all registered exit actions for the source state.
	// Returns an error if any exit action fails.
	ExecuteExitActions(from, to string) error

	// ExecuteTransitionActions runs all registered transition actions.
	// Returns an error if any transition action fails.
	ExecuteTransitionActions(from, to string) error

	// ExecuteEntryActions runs all registered entry actions for the target state.
	// Panics are recovered and logged but do not propagate.
	ExecuteEntryActions(from, to string)

	// ExecutePostTransitionHooks runs all registered post-transition hooks.
	// Panics are recovered and logged but do not propagate.
	ExecutePostTransitionHooks(from, to string)
}
