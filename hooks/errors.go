package hooks

import "errors"

var (
	// ErrCallbackFailed is returned when a callback fails during transition
	ErrCallbackFailed = errors.New("callback failed")

	// ErrCallbackPanic is returned when a callback panics
	ErrCallbackPanic = errors.New("callback panicked")

	// ErrHookNameEmpty is returned when attempting to register a hook with an empty name
	ErrHookNameEmpty = errors.New("hook name cannot be empty")

	// ErrHookNameAlreadyExists is returned when attempting to register a hook with a name that already exists
	ErrHookNameAlreadyExists = errors.New("hook with this name already exists")

	// ErrHookNotFound is returned when attempting to remove a hook that doesn't exist
	ErrHookNotFound = errors.New("hook not found")
)
