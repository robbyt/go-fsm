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
	"errors"
)

// CallbackFunc is the signature for callbacks that can abort transitions.
// Returns an error to reject the transition.
type CallbackFunc func(ctx context.Context, from, to string) error

// ActionFunc is the signature for callbacks that cannot abort transitions.
// These execute after the point of no return and are used for side effects only.
type ActionFunc func(ctx context.Context, from, to string)

var (
	// ErrGuardRejected is returned when a guard condition rejects a transition
	ErrGuardRejected = errors.New("guard rejected transition")

	// ErrCallbackFailed is returned when a callback fails during transition
	ErrCallbackFailed = errors.New("callback failed")

	// ErrCallbackPanic is returned when a callback panics
	ErrCallbackPanic = errors.New("callback panicked")
)
