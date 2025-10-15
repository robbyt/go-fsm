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

package transitions

import "errors"

var (
	// ErrEmptyConfig is returned when a transitions configuration is empty or nil.
	ErrEmptyConfig = errors.New("transitions config cannot be empty")

	// ErrEmptyStateName is returned when a state name is an empty string.
	ErrEmptyStateName = errors.New("state name cannot be empty")

	// ErrWhitespaceStateName is returned when a state name contains only whitespace.
	ErrWhitespaceStateName = errors.New("state name cannot be whitespace-only")

	// ErrInvalidWhitespace is returned when a state name has leading or trailing whitespace.
	ErrInvalidWhitespace = errors.New("state name cannot have leading or trailing whitespace")

	// ErrUndefinedDestinations is returned when destination states are not defined as source states in the config.
	ErrUndefinedDestinations = errors.New("destination states not defined in config")

	// ErrInvalidConfig is returned when the transitions configuration is invalid.
	ErrInvalidConfig = errors.New("invalid transitions config")
)
