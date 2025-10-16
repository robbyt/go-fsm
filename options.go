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
	"fmt"
	"log/slog"
)

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

// WithLogger sets the logger for the FSM.
func WithLogger(logger *slog.Logger) Option {
	return func(m *Machine) error {
		if logger == nil {
			return fmt.Errorf("logger cannot be nil")
		}
		m.logger = logger
		m.logHandler = logger.Handler()
		return nil
	}
}

// WithLogHandler creates a new slog instance for the FSM using your slog.Handler implementation.
func WithLogHandler(handler slog.Handler) Option {
	return func(m *Machine) error {
		if handler == nil {
			return fmt.Errorf("log handler cannot be nil")
		}
		m.logHandler = handler
		m.logger = slog.New(handler)
		return nil
	}
}
