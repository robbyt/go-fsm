package hooks

import (
	"fmt"
	"log/slog"

	"github.com/robbyt/go-fsm/v2/transitions"
)

// Option is a functional option for configuring SynchronousCallbackRegistry.
type Option func(*Registry) error

// WithLogger sets the logger for the registry.
func WithLogger(logger *slog.Logger) Option {
	return func(r *Registry) error {
		if logger == nil {
			return fmt.Errorf("logger cannot be nil")
		}
		r.logger = logger
		return nil
	}
}

// WithLogHandler create a new slog instance for this registry using your slog.Handler implementation.
func WithLogHandler(handler slog.Handler) Option {
	return func(r *Registry) error {
		if handler == nil {
			return fmt.Errorf("log handler cannot be nil")
		}
		r.logger = slog.New(handler)
		return nil
	}
}

// WithTransitions sets the state transition table for pattern validation and expansion.
func WithTransitions(trans *transitions.Config) Option {
	return func(r *Registry) error {
		if trans == nil {
			return fmt.Errorf("transitions cannot be nil")
		}
		r.transitions = trans
		r.allStates = r.getAllStatesFromTransitions()
		return nil
	}
}
