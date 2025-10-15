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

// Option is a functional option for configuring a Machine during construction.
type Option func(*Machine) error

// WithCallbackRegistry sets a custom callback registry implementation.
// The registry must be configured before passing to the FSM.
func WithCallbackRegistry(executor CallbackExecutor) Option {
	return func(m *Machine) error {
		m.callbacks = executor
		return nil
	}
}
