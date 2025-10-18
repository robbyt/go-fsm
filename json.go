package fsm

import (
	"encoding/json"
	"fmt"

	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/transitions"
)

// transitionSerializer is an optional interface for transitions that support JSON serialization.
// Only transitions implementing this interface can be marshaled to JSON.
type transitionSerializer interface {
	AsMap() map[string][]string
}

// hookSerializer is an optional interface for callback executors that support JSON serialization.
// Only callback executors implementing this interface will have hooks included in JSON output.
type hookSerializer interface {
	GetHooks() []hooks.HookInfo
}

// persistentState is used for JSON marshaling/unmarshaling.
type persistentState struct {
	State       string              `json:"state"`
	Transitions map[string][]string `json:"transitions"`
	Hooks       []hooks.HookInfo    `json:"hooks,omitempty"`
}

// NewFromJSON creates a new FSM by deserializing from JSON.
// The JSON must contain state and transitions fields. Any hooks in the JSON are ignored with a warning.
// Options can be provided to configure logger and callback registry after deserialization.
//
// Example usage:
//
//	jsonData := []byte(`{"state":"running","transitions":{"running":["stopped"],"stopped":[]}}`)
//	fsm, err := fsm.NewFromJSON(jsonData, fsm.WithLogger(logger))
func NewFromJSON(data []byte, opts ...Option) (*Machine, error) {
	var pState persistentState
	if err := json.Unmarshal(data, &pState); err != nil {
		return nil, fmt.Errorf("failed to unmarshal FSM: %w", err)
	}

	if pState.State == "" {
		return nil, ErrEmptyState
	}

	if len(pState.Transitions) == 0 {
		return nil, ErrEmptyTransitions
	}

	// Create transitions.Config from the map
	transConfig, err := transitions.New(pState.Transitions)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidJSONTransitions, err)
	}

	// Validate that the state exists in transitions
	if !transConfig.HasState(pState.State) {
		return nil, fmt.Errorf("%w: state '%s' is not defined in transitions", ErrInvalidConfiguration, pState.State)
	}

	// Create the FSM using the standard constructor
	fsm, err := New(pState.State, transConfig, opts...)
	if err != nil {
		return nil, err
	}

	// Warn if hooks were present in JSON
	if len(pState.Hooks) > 0 {
		fsm.logger.Warn("hooks from JSON will be dropped and must be re-registered",
			"hook_count", len(pState.Hooks))
	}

	return fsm, nil
}

// MarshalJSON implements the json.Marshaler interface.
// It serializes the current state, all transitions, and registered hooks.
// Transitions must implement transitionSerializer interface to be marshaled.
func (fsm *Machine) MarshalJSON() ([]byte, error) {
	fsm.mutex.RLock()
	defer fsm.mutex.RUnlock()

	currentState := fsm.GetState()

	// Get transitions as a map via optional serialization interface
	serializer, ok := fsm.transitions.(transitionSerializer)
	if !ok {
		return nil, fmt.Errorf("transitions do not implement transitionSerializer interface, cannot marshal to JSON")
	}
	transitionsMap := serializer.AsMap()

	pState := persistentState{
		State:       currentState,
		Transitions: transitionsMap,
	}

	// Get hooks if a registry is configured and supports serialization
	if fsm.callbacks != nil {
		if hookSer, ok := fsm.callbacks.(hookSerializer); ok {
			pState.Hooks = hookSer.GetHooks()
		}
	}

	jsonData, err := json.Marshal(pState)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FSM state: %w", err)
	}

	return jsonData, nil
}
