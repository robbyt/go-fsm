package hooks

import "errors"

var (
	// ErrCallbackFailed is returned when a callback fails during transition
	ErrCallbackFailed = errors.New("callback failed")

	// ErrCallbackPanic is returned when a callback panics
	ErrCallbackPanic = errors.New("callback panicked")
)
