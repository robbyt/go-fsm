package fsm

import (
	"encoding/json" // Added for JSON tests
	"log/slog"      // Added for JSON tests (handler)
	"os"            // Added for JSON tests (handler)
	"testing"

	"github.com/robbyt/go-fsm/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSM(t *testing.T) {
	t.Parallel()

	t.Run("NewFSM with invalid initial status", func(t *testing.T) {
		fsm, err := New(nil, "bla", transitions.TypicalTransitions)
		assert.Nil(t, fsm)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidState) // More specific check
	})

	t.Run("NewFSM with nil allowedTransitions", func(t *testing.T) {
		fsm, err := New(nil, transitions.StatusNew, nil)
		assert.Nil(t, fsm)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrAvailableStateData) // More specific check
	})

	t.Run("GetState and SetState", func(t *testing.T) {
		fsm, err := New(nil, transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		assert.Equal(t, transitions.StatusNew, fsm.GetState())

		err = fsm.SetState(transitions.StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, transitions.StatusRunning, fsm.GetState())

		// Test setting an invalid state (not defined as a source state)
		err = fsm.SetState("invalid_state")
		require.ErrorIs(t, err, ErrInvalidState)
		assert.Equal(t, transitions.StatusRunning, fsm.GetState()) // State should not change
	})

	t.Run("Transition", func(t *testing.T) {
		testCases := []struct {
			name          string
			initialState  string
			toState       string
			expectedErr   error
			expectedState string
		}{
			{
				name:          "Valid transition from transitions.StatusNew to transitions.StatusBooting",
				initialState:  transitions.StatusNew,
				toState:       transitions.StatusBooting,
				expectedErr:   nil,
				expectedState: transitions.StatusBooting,
			},
			{
				name:          "Invalid transition from transitions.StatusNew to transitions.StatusRunning",
				initialState:  transitions.StatusNew,
				toState:       transitions.StatusRunning,
				expectedErr:   ErrInvalidStateTransition,
				expectedState: transitions.StatusNew,
			},
			{
				name:          "Invalid target state", // Added case
				initialState:  transitions.StatusNew,
				toState:       "NonExistentState",
				expectedErr:   ErrInvalidStateTransition, // Transition is invalid as target doesn't exist in map value
				expectedState: transitions.StatusNew,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				fsm, err := New(nil, tc.initialState, transitions.TypicalTransitions)
				require.NoError(t, err)

				err = fsm.Transition(tc.toState)

				if tc.expectedErr != nil {
					require.ErrorIs(t, err, tc.expectedErr)
				} else {
					require.NoError(t, err)
				}

				assert.Equal(t, tc.expectedState, fsm.GetState())
			})
		}
	})

	t.Run("TransitionIfCurrentState", func(t *testing.T) {
		testCases := []struct {
			name          string
			initialState  string
			fromState     string
			toState       string
			expectedErr   error
			expectedState string
		}{
			{
				name:          "Valid transition with matching current state",
				initialState:  transitions.StatusNew,
				fromState:     transitions.StatusNew,
				toState:       transitions.StatusBooting,
				expectedErr:   nil,
				expectedState: transitions.StatusBooting,
			},
			{
				name:          "Invalid transition due to mismatched current state",
				initialState:  transitions.StatusBooting,
				fromState:     transitions.StatusNew,
				toState:       transitions.StatusRunning, // This transition would be valid if state matched
				expectedErr:   ErrCurrentStateIncorrect,
				expectedState: transitions.StatusBooting,
			},
			{
				name:          "Invalid transition due to invalid state transition rule",
				initialState:  transitions.StatusNew,
				fromState:     transitions.StatusNew,
				toState:       transitions.StatusRunning, // Invalid transition from New -> Running
				expectedErr:   ErrInvalidStateTransition,
				expectedState: transitions.StatusNew,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				fsm, err := New(nil, tc.initialState, transitions.TypicalTransitions)
				require.NoError(t, err)

				err = fsm.TransitionIfCurrentState(tc.fromState, tc.toState)

				if tc.expectedErr != nil {
					require.ErrorIs(t, err, tc.expectedErr)
				} else {
					require.NoError(t, err)
				}

				assert.Equal(t, tc.expectedState, fsm.GetState())
			})
		}
	})
}

func TestFSM_Transition_DisallowedStateChange(t *testing.T) {
	t.Parallel()

	fsm, err := New(nil, transitions.StatusNew, transitions.TypicalTransitions)
	require.NoError(t, err)

	// Attempt transition to a state not allowed from transitions.StatusNew
	err = fsm.Transition(transitions.StatusRunning)

	require.ErrorIs(t, err, ErrInvalidStateTransition)
	assert.Equal(t, transitions.StatusNew, fsm.GetState()) // State remains unchanged
}

func TestFSM_NoAllowedTransitions(t *testing.T) {
	t.Parallel()

	// Create a small transition configuration with limited states
	smallestTransitions := transitions.MustNew(map[string][]string{
		transitions.StatusNew:   {transitions.StatusError},
		transitions.StatusError: {}, // transitions.StatusError has no outgoing transitions
	})
	fsm, err := New(nil, transitions.StatusNew, smallestTransitions)
	require.NoError(t, err)

	// Verify initial state
	assert.Equal(t, transitions.StatusNew, fsm.GetState())

	// Transition to transitions.StatusError
	err = fsm.Transition(transitions.StatusError)
	require.NoError(t, err)
	assert.Equal(t, transitions.StatusError, fsm.GetState())

	// Attempt invalid transition from transitions.StatusError (no transitions defined)
	err = fsm.Transition(transitions.StatusNew)
	require.ErrorIs(t, err, ErrInvalidStateTransition)
	assert.Equal(t, transitions.StatusError, fsm.GetState(), "State should not change after failed transition")
}

func TestFSM_RaceCondition_StateTransitions(t *testing.T) {
	t.Parallel()

	// Define states for testing
	const (
		StateA = "StateA"
		StateB = "StateB"
		StateC = "StateC"
		StateD = "StateD"
		StateE = "StateE"
		StateF = "StateF"
		StateG = "StateG"
	)

	// Define transitions that form a chain
	trans := transitions.MustNew(map[string][]string{
		StateA: {StateB},
		StateB: {StateC},
		StateC: {StateD},
		StateD: {StateE},
		StateE: {StateF},
		StateF: {StateG},
		StateG: {StateA},
	})

	// Create the FSM starting at "StateA"
	fsmMachine, err := New(nil, StateA, trans)
	require.NoError(t, err)

	// Verify initial state
	assert.Equal(t, StateA, fsmMachine.GetState())

	// Perform a series of transitions
	stateSequence := []string{StateB, StateC, StateD, StateE, StateF, StateG}
	for _, state := range stateSequence {
		err := fsmMachine.Transition(state)
		require.NoError(t, err)
		assert.Equal(t, state, fsmMachine.GetState())
	}

	// Final transition back to StateA
	err = fsmMachine.Transition(StateA)
	require.NoError(t, err)
	assert.Equal(t, StateA, fsmMachine.GetState())
}

func TestFSM_TransitionBool(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		initialState   string
		toState        string
		expectedResult bool
		expectedState  string
	}{
		{
			name:           "Valid transition from transitions.StatusNew to transitions.StatusBooting",
			initialState:   transitions.StatusNew,
			toState:        transitions.StatusBooting,
			expectedResult: true,
			expectedState:  transitions.StatusBooting,
		},
		{
			name:           "Invalid transition from transitions.StatusNew to transitions.StatusRunning",
			initialState:   transitions.StatusNew,
			toState:        transitions.StatusRunning,
			expectedResult: false,
			expectedState:  transitions.StatusNew,
		},
		{
			name:           "Valid transition from transitions.StatusRunning to transitions.StatusReloading",
			initialState:   transitions.StatusRunning,
			toState:        transitions.StatusReloading,
			expectedResult: true,
			expectedState:  transitions.StatusReloading,
		},
		{
			name:           "Invalid transition from transitions.StatusRunning to transitions.StatusNew",
			initialState:   transitions.StatusRunning,
			toState:        transitions.StatusNew,
			expectedResult: false,
			expectedState:  transitions.StatusRunning,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fsm, err := New(nil, tc.initialState, transitions.TypicalTransitions)
			require.NoError(t, err)

			result := fsm.TransitionBool(tc.toState)
			assert.Equal(t, tc.expectedResult, result)
			assert.Equal(t, tc.expectedState, fsm.GetState())
		})
	}
}

func TestFSM_JSONPersistence(t *testing.T) {
	t.Parallel()

	// Use a discard handler for tests unless specific log output is needed
	testHandler := slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{Level: slog.LevelError},
	) // Change to os.Stdout and LevelDebug to see logs

	t.Run("MarshalJSON", func(t *testing.T) {
		initialState := transitions.StatusRunning
		fsm, err := New(testHandler, initialState, transitions.TypicalTransitions)
		require.NoError(t, err)

		// Perform a transition to ensure state changes are captured if needed
		err = fsm.Transition(transitions.StatusReloading)
		require.NoError(t, err)
		currentState := transitions.StatusReloading // The state we expect to be marshaled

		jsonData, err := json.Marshal(fsm)
		require.NoError(t, err)

		// Expected JSON structure: {"state":"transitions.StatusReloading"}
		expectedJSON := `{"state":"` + currentState + `"}`
		assert.JSONEq(t, expectedJSON, string(jsonData))
	})

	t.Run("NewFromJSON - Success", func(t *testing.T) {
		originalState := transitions.StatusRunning
		fsmOrig, err := New(testHandler, transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)
		err = fsmOrig.Transition(transitions.StatusBooting) // Transition a few times
		require.NoError(t, err)
		err = fsmOrig.Transition(originalState)
		require.NoError(t, err)

		// Marshal the original FSM
		jsonData, err := json.Marshal(fsmOrig)
		require.NoError(t, err)

		// Restore using NewFromJSON
		fsmRestored, err := NewFromJSON(testHandler, jsonData, transitions.TypicalTransitions)
		require.NoError(t, err)
		require.NotNil(t, fsmRestored)

		// Verify the state of the restored FSM
		assert.Equal(t, originalState, fsmRestored.GetState())

		// Optional: Verify transitions still work on the restored machine
		err = fsmRestored.Transition(transitions.StatusReloading)
		require.NoError(t, err)
		assert.Equal(t, transitions.StatusReloading, fsmRestored.GetState())
	})

	t.Run("NewFromJSON - Invalid JSON Data", func(t *testing.T) {
		invalidJSON := []byte(`{"state":`) // Malformed JSON

		fsmRestored, err := NewFromJSON(testHandler, invalidJSON, transitions.TypicalTransitions)
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		// Check if the error is related to JSON unmarshaling (it should be)
		assert.Contains(t, err.Error(), "failed to unmarshal FSM JSON data")
	})

	t.Run("NewFromJSON - Unknown State in JSON", func(t *testing.T) {
		unknownStateJSON := []byte(
			`{"state":"transitions.StatusUnknown"}`,
		) // Assume transitions.StatusUnknown is not in transitions.TypicalTransitions

		fsmRestored, err := NewFromJSON(testHandler, unknownStateJSON, transitions.TypicalTransitions)
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		// NewFromJSON calls New, which validates the initial state.
		// Expect ErrInvalidState wrapped in the NewFromJSON error message.
		require.ErrorIs(t, err, ErrInvalidState)
		assert.Contains(
			t,
			err.Error(),
			"failed to initialize FSM with restored state 'transitions.StatusUnknown'",
		)
	})

	t.Run("NewFromJSON - Nil Transitions Config", func(t *testing.T) {
		// Marshal a valid state first
		fsmOrig, err := New(testHandler, transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)
		jsonData, err := json.Marshal(fsmOrig)
		require.NoError(t, err)

		// Attempt to restore with nil transitions
		fsmRestored, err := NewFromJSON(testHandler, jsonData, nil)
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		// NewFromJSON calls New, which returns ErrAvailableStateData for nil transitions.
		require.ErrorIs(t, err, ErrAvailableStateData)
		assert.Contains(
			t,
			err.Error(),
			"failed to initialize FSM with restored state",
		) // Check wrapping
	})

	t.Run("NewFromJSON - Empty JSON Data", func(t *testing.T) {
		emptyJSON := []byte(`{}`) // Valid JSON, but missing "state" field

		fsmRestored, err := NewFromJSON(testHandler, emptyJSON, transitions.TypicalTransitions)
		// The unmarshal step will succeed, but pState.State will be "" (empty string)
		// The subsequent call to New("", transitions.TypicalTransitions) should fail validation.
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		require.ErrorIs(t, err, ErrInvalidState) // New should return ErrInvalidState for ""
		assert.Contains(t, err.Error(), "failed to initialize FSM with restored state ''")
	})
}

func TestFSM_GetAllStates(t *testing.T) {
	t.Parallel()

	t.Run("GetAllStates with transitions.TypicalTransitions", func(t *testing.T) {
		fsm, err := New(nil, transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		states := fsm.GetAllStates()

		expectedStates := []string{
			transitions.StatusNew, transitions.StatusBooting, transitions.StatusRunning, transitions.StatusReloading,
			transitions.StatusStopping, transitions.StatusStopped, transitions.StatusError, transitions.StatusUnknown,
		}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, len(expectedStates))
	})

	t.Run("GetAllStates with custom transitions", func(t *testing.T) {
		customTransitions := transitions.MustNew(map[string][]string{
			"StateA": {"StateB", "StateC"},
			"StateB": {"StateC"},
			"StateC": {"StateA"},
		})

		fsm, err := New(nil, "StateA", customTransitions)
		require.NoError(t, err)

		states := fsm.GetAllStates()
		expectedStates := []string{"StateA", "StateB", "StateC"}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, 3)
	})

	t.Run("GetAllStates with single state", func(t *testing.T) {
		singleStateTransitions := transitions.MustNew(map[string][]string{
			"OnlyState": {},
		})

		fsm, err := New(nil, "OnlyState", singleStateTransitions)
		require.NoError(t, err)

		states := fsm.GetAllStates()
		expectedStates := []string{"OnlyState"}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, 1)
	})

	t.Run("GetAllStates returns copy not reference", func(t *testing.T) {
		fsm, err := New(nil, transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		states1 := fsm.GetAllStates()
		states2 := fsm.GetAllStates()

		assert.ElementsMatch(t, states1, states2)

		// Modify one slice to ensure they're independent
		states1[0] = "ModifiedState"
		assert.NotEqual(t, states1[0], states2[0])
	})
}
