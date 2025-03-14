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

// TransitionsConfig represents a configuration for allowed transitions the FSM.
type TransitionsConfig map[string][]string

// transitionIndex is a map of maps, the second map is like a set, used for
type transitionIndex map[string]map[string]struct{}

// makeIndex called during fsm creation, creates a transitionIndex from a TransitionsConfig.
func makeIndex(transCfg TransitionsConfig) transitionIndex {
	t := make(transitionIndex)

	for from, to := range transCfg {
		if _, ok := t[from]; !ok {
			t[from] = make(map[string]struct{})
		}
		for _, transition := range to {
			t[from][transition] = struct{}{}
		}
	}

	return t
}

// Collection of common statuses
const (
	StatusNew       = "New"
	StatusBooting   = "Booting"
	StatusRunning   = "Running"
	StatusReloading = "Reloading"
	StatusStopping  = "Stopping"
	StatusStopped   = "Stopped"
	StatusError     = "Error"
	StatusUnknown   = "Unknown"
)

// TypicalTransitions is a common set of transitions, useful as a guide. Each key is the current
// state, and the value is a list of valid next states the FSM can transition to.
var TypicalTransitions = TransitionsConfig{
	StatusNew:       {StatusBooting, StatusError},
	StatusBooting:   {StatusRunning, StatusError},
	StatusRunning:   {StatusReloading, StatusStopping, StatusError},
	StatusReloading: {StatusRunning, StatusError},
	StatusStopping:  {StatusStopped, StatusError},
	StatusStopped:   {StatusNew, StatusError},
	StatusError:     {StatusError, StatusStopping, StatusStopped},
	StatusUnknown:   {StatusUnknown},
}
