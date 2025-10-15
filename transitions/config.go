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

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Config represents a configuration for allowed state transitions in the FSM.
// It stores both the original configuration and a pre-built index for efficient lookups.
type Config struct {
	config map[string][]string
	index  map[string]map[string]struct{}
}

// buildIndex creates the internal lookup index from the configuration map.
// It validates that all destination states are explicitly defined as source states in the config.
// Returns all validation errors joined together.
func buildIndex(config map[string][]string) (map[string]map[string]struct{}, error) {
	index := make(map[string]map[string]struct{})
	undefinedStates := make(map[string]struct{})
	var errs []error

	// Build the index and validate state names
	for from, tos := range config {
		// Validate source state name
		if err := validateStateName(from); err != nil {
			errs = append(errs, fmt.Errorf("invalid source state: %w", err))
			// Skip this source state but continue validating others
			continue
		}

		index[from] = make(map[string]struct{})

		for _, to := range tos {
			// Validate destination state name
			if err := validateStateName(to); err != nil {
				errs = append(errs, fmt.Errorf("invalid destination state: %w", err))
				// Skip this destination but continue validating others
				continue
			}

			index[from][to] = struct{}{}

			// Check if destination state is defined as a source state
			if _, isDefined := config[to]; !isDefined {
				undefinedStates[to] = struct{}{}
			}
		}
	}

	// Add error for undefined destination states
	if len(undefinedStates) > 0 {
		errs = append(errs, fmt.Errorf(
			"%w: %v (hint: define them as source states with empty transitions for terminal states)",
			ErrUndefinedDestinations,
			slices.Sorted(maps.Keys(undefinedStates)),
		))
	}

	return index, errors.Join(errs...)
}

// validateStateName checks if a state name is valid.
// Returns an error if the state name is empty or contains only whitespace.
func validateStateName(state string) error {
	if state == "" {
		return ErrEmptyStateName
	}
	if strings.TrimSpace(state) == "" {
		return fmt.Errorf("%w: %q", ErrWhitespaceStateName, state)
	}
	if strings.TrimSpace(state) != state {
		return fmt.Errorf("%w: %q", ErrInvalidWhitespace, state)
	}
	return nil
}

// New creates a new Transitions instance from a configuration map.
// The configuration maps source states to their allowed target states.
// Returns an error if the configuration is empty or contains undefined destination states.
func New(config map[string][]string) (*Config, error) {
	if len(config) == 0 {
		return nil, ErrEmptyConfig
	}

	index, err := buildIndex(config)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	return &Config{
		config: config,
		index:  index,
	}, nil
}

// MustNew creates a new Transitions instance and panics if there's an error.
func MustNew(config map[string][]string) *Config {
	t, err := New(config)
	if err != nil {
		panic(fmt.Sprintf("failed to create transitions: %v", err))
	}
	return t
}

// IsTransitionAllowed checks if a transition from one state to another is allowed.
func (t *Config) IsTransitionAllowed(from, to string) bool {
	allowedTargets, ok := t.index[from]
	if !ok {
		return false
	}
	_, exists := allowedTargets[to]
	return exists
}

// GetAllowedTransitions returns all allowed target states from a given source state.
// Returns the slice of target states and a boolean indicating if the source state exists.
func (t *Config) GetAllowedTransitions(from string) ([]string, bool) {
	targetsMap, ok := t.index[from]
	if !ok {
		return nil, false
	}

	targets := make([]string, 0, len(targetsMap))
	for target := range targetsMap {
		targets = append(targets, target)
	}
	return targets, true
}

// HasState checks if a state exists as a source state in the transitions configuration.
func (t *Config) HasState(state string) bool {
	_, ok := t.index[state]
	return ok
}

// GetAllStates returns all unique states defined in the transitions configuration.
// This includes both source states and target states.
func (t *Config) GetAllStates() []string {
	stateSet := make(map[string]struct{})

	// Add all source states
	for from := range t.config {
		stateSet[from] = struct{}{}
	}

	// Add all target states
	for _, targets := range t.config {
		for _, to := range targets {
			stateSet[to] = struct{}{}
		}
	}

	return slices.Collect(maps.Keys(stateSet))
}

// AsMap returns a copy of the configuration map.
func (t *Config) AsMap() map[string][]string {
	result := maps.Clone(t.config)
	for from, tos := range result {
		result[from] = slices.Clone(tos)
	}
	return result
}

// MarshalJSON implements json.Marshaler interface.
// It serializes only the config map, as the index can be rebuilt on unmarshal.
func (t *Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.config)
}

// UnmarshalJSON implements json.Unmarshaler interface.
// It deserializes the config map and rebuilds the internal index.
func (t *Config) UnmarshalJSON(data []byte) error {
	var config map[string][]string
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal transitions config: %w", err)
	}

	if len(config) == 0 {
		return ErrEmptyConfig
	}

	index, err := buildIndex(config)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	t.config = config
	t.index = index
	return nil
}
