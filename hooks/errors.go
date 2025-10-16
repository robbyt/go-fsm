package hooks

import "errors"

var (
	// ErrGuardRejected is returned when a guard condition rejects a transition
	ErrGuardRejected = errors.New("guard rejected transition")

	// ErrCallbackFailed is returned when a callback fails during transition
	ErrCallbackFailed = errors.New("callback failed")

	// ErrCallbackPanic is returned when a callback panics
	ErrCallbackPanic = errors.New("callback panicked")
)
