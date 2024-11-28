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

import "errors"

// ErrAvailableStateData is returned when the available state data is not correctly formatted
var ErrAvailableStateData = errors.New("available state data is malformed")

// ErrCurrentStateIncorrect is returned when the current state does not match with expectations
var ErrCurrentStateIncorrect = errors.New("current state is incorrect")

// ErrInvalidState is returned when the state is somehow invalid
var ErrInvalidState = errors.New("state is invalid")

// ErrInvalidStateTransition is returned when the state transition is invalid
var ErrInvalidStateTransition = errors.New("state transition is invalid")
