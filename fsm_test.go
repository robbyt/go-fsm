package fsm

import (
	"context"
	"encoding/json" // Added for JSON tests
	"log/slog"      // Added for JSON tests (handler)
	"os"            // Added for JSON tests (handler)
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSM(t *testing.T) {
	t.Parallel()

	t.Run("NewFSM with invalid initial status", func(t *testing.T) {
		fsm, err := New(nil, "bla", TypicalTransitions)
		assert.Nil(t, fsm)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidState) // More specific check
	})

	t.Run("NewFSM with nil allowedTransitions", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, nil)
		assert.Nil(t, fsm)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAvailableStateData) // More specific check
	})

	t.Run("GetState and SetState", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		assert.Equal(t, StatusNew, fsm.GetState())

		err = fsm.SetState(StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, StatusRunning, fsm.GetState())

		// Test setting an invalid state (not defined as a source state)
		err = fsm.SetState("invalid_state")
		assert.ErrorIs(t, err, ErrInvalidState)
		assert.Equal(t, StatusRunning, fsm.GetState()) // State should not change
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
				name:          "Valid transition from StatusNew to StatusBooting",
				initialState:  StatusNew,
				toState:       StatusBooting,
				expectedErr:   nil,
				expectedState: StatusBooting,
			},
			{
				name:          "Invalid transition from StatusNew to StatusRunning",
				initialState:  StatusNew,
				toState:       StatusRunning,
				expectedErr:   ErrInvalidStateTransition,
				expectedState: StatusNew,
			},
			{
				name:          "Invalid target state", // Added case
				initialState:  StatusNew,
				toState:       "NonExistentState",
				expectedErr:   ErrInvalidStateTransition, // Transition is invalid as target doesn't exist in map value
				expectedState: StatusNew,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				fsm, err := New(nil, tc.initialState, TypicalTransitions)
				require.NoError(t, err)

				err = fsm.Transition(tc.toState)

				if tc.expectedErr != nil {
					assert.ErrorIs(t, err, tc.expectedErr)
				} else {
					assert.NoError(t, err)
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
				initialState:  StatusNew,
				fromState:     StatusNew,
				toState:       StatusBooting,
				expectedErr:   nil,
				expectedState: StatusBooting,
			},
			{
				name:          "Invalid transition due to mismatched current state",
				initialState:  StatusBooting,
				fromState:     StatusNew,
				toState:       StatusRunning, // This transition would be valid if state matched
				expectedErr:   ErrCurrentStateIncorrect,
				expectedState: StatusBooting,
			},
			{
				name:          "Invalid transition due to invalid state transition rule",
				initialState:  StatusNew,
				fromState:     StatusNew,
				toState:       StatusRunning, // Invalid transition from New -> Running
				expectedErr:   ErrInvalidStateTransition,
				expectedState: StatusNew,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				fsm, err := New(nil, tc.initialState, TypicalTransitions)
				require.NoError(t, err)

				err = fsm.TransitionIfCurrentState(tc.fromState, tc.toState)

				if tc.expectedErr != nil {
					assert.ErrorIs(t, err, tc.expectedErr)
				} else {
					assert.NoError(t, err)
				}

				assert.Equal(t, tc.expectedState, fsm.GetState())
			})
		}
	})
}

func TestFSM_Transition_DisallowedStateChange(t *testing.T) {
	t.Parallel()

	fsm, err := New(nil, StatusNew, TypicalTransitions)
	require.NoError(t, err)

	// Attempt transition to a state not allowed from StatusNew
	err = fsm.Transition(StatusRunning)

	assert.ErrorIs(t, err, ErrInvalidStateTransition)
	assert.Equal(t, StatusNew, fsm.GetState()) // State remains unchanged
}

// This test modifies internal state, which is generally discouraged,
// but kept here as it was in the original file.
func TestFSM_Transition_ModifiedAllowedTransitions(t *testing.T) {
	t.Parallel()

	fsm, err := New(nil, StatusNew, TypicalTransitions)
	require.NoError(t, err)

	// Directly manipulate the internal index (fragile test)
	fsm.mutex.Lock()
	delete(fsm.transitionIndex, StatusNew)
	fsm.mutex.Unlock()

	t.Run("Transition with removed allowedTransitions", func(t *testing.T) {
		err := fsm.Transition(StatusBooting)
		// Now that StatusNew is not in the index, the error should be ErrInvalidState
		assert.ErrorIs(t, err, ErrInvalidState)
		assert.Equal(t, StatusNew, fsm.GetState()) // State remains unchanged
	})
}

func TestFSM_NoAllowedTransitions(t *testing.T) {
	t.Parallel() // Added parallel execution

	// Create a small transition configuration with limited states
	smallestTransitions := TransitionsConfig{
		StatusNew:   {StatusError},
		StatusError: {}, // StatusError has no outgoing transitions
	}
	fsm, err := New(nil, StatusNew, smallestTransitions)
	require.NoError(t, err)

	// Create a context with timeout to prevent test from hanging
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get state channel and read initial state
	listener := fsm.GetStateChan(ctx)
	require.NotNil(t, listener, "Listener channel should not be nil") // Added nil check

	select {
	case initialState := <-listener:
		assert.Equal(t, StatusNew, initialState, "First state should be the initial state")
	case <-ctx.Done():
		t.Fatal("Timed out waiting for initial state")
	}

	// Transition to StatusError
	err = fsm.Transition(StatusError)
	require.NoError(t, err)
	assert.Equal(t, StatusError, fsm.GetState())

	// Read the updated state from channel
	select {
	case updatedState := <-listener:
		assert.Equal(t, StatusError, updatedState, "Should receive state transition to StatusError")
	case <-ctx.Done():
		t.Fatal("Timed out waiting for state update to StatusError")
	}

	// Attempt invalid transition from StatusError (no transitions defined)
	err = fsm.Transition(StatusNew)
	require.ErrorIs(t, err, ErrInvalidStateTransition)
	assert.Equal(t, StatusError, fsm.GetState(), "State should not change after failed transition")

	// Verify no state update was sent after the failed transition
	select {
	case unexpectedState := <-listener:
		t.Fatalf("No state transition expected, but received: %s", unexpectedState) // Use Fatalf
	case <-time.After(100 * time.Millisecond):
		// All good, no state update received
	}
}

func TestFSM_RaceCondition_Broadcast(t *testing.T) {
	t.Parallel() // Added parallel execution

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
	transitions := TransitionsConfig{
		StateA: {StateB},
		StateB: {StateC},
		StateC: {StateD},
		StateD: {StateE},
		StateE: {StateF},
		StateF: {StateG},
		StateG: {StateA},
	}

	// Create the FSM starting at "StateA"
	fsmMachine, err := New(nil, StateA, transitions)
	require.NoError(t, err)

	// Create a context with timeout to prevent test from hanging
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Create a listener for the FSM state changes
	listener := fsmMachine.GetStateChan(ctx)
	require.NotNil(t, listener, "Listener channel should not be nil") // Added nil check

	// Read initial state
	select {
	case initialState := <-listener:
		assert.Equal(t, StateA, initialState)
	case <-ctx.Done():
		t.Fatal("Timed out waiting for initial state")
	}

	// Perform a series of transitions
	stateSequence := []string{StateB, StateC, StateD, StateE, StateF, StateG}
	for _, state := range stateSequence {
		err := fsmMachine.Transition(state)
		require.NoError(t, err) // Use require for critical steps

		// Verify the state changed correctly
		assert.Equal(t, state, fsmMachine.GetState())

		// Read the state update from channel with timeout
		select {
		case receivedState := <-listener:
			assert.Equal(t, state, receivedState)
		case <-ctx.Done(): // Check context cancellation
			t.Fatalf("Context cancelled while waiting for state update to %s", state)
		case <-time.After(500 * time.Millisecond): // Increased timeout slightly
			t.Fatalf("Timed out waiting for state update to %s", state) // Use Fatalf
		}
	}

	// Final transition back to StateA
	err = fsmMachine.Transition(StateA)
	require.NoError(t, err) // Use require
	assert.Equal(t, StateA, fsmMachine.GetState())

	select {
	case receivedState := <-listener:
		assert.Equal(t, StateA, receivedState)
	case <-ctx.Done(): // Check context cancellation
		t.Fatal("Context cancelled while waiting for final state update")
	case <-time.After(500 * time.Millisecond): // Increased timeout slightly
		t.Fatal("Timed out waiting for final state update") // Use Fatal
	}
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
			name:           "Valid transition from StatusNew to StatusBooting",
			initialState:   StatusNew,
			toState:        StatusBooting,
			expectedResult: true,
			expectedState:  StatusBooting,
		},
		{
			name:           "Invalid transition from StatusNew to StatusRunning",
			initialState:   StatusNew,
			toState:        StatusRunning,
			expectedResult: false,
			expectedState:  StatusNew,
		},
		{
			name:           "Valid transition from StatusRunning to StatusReloading",
			initialState:   StatusRunning,
			toState:        StatusReloading,
			expectedResult: true,
			expectedState:  StatusReloading,
		},
		{
			name:           "Invalid transition from StatusRunning to StatusNew",
			initialState:   StatusRunning,
			toState:        StatusNew,
			expectedResult: false,
			expectedState:  StatusRunning,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fsm, err := New(nil, tc.initialState, TypicalTransitions)
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
		initialState := StatusRunning
		fsm, err := New(testHandler, initialState, TypicalTransitions)
		require.NoError(t, err)

		// Perform a transition to ensure state changes are captured if needed
		err = fsm.Transition(StatusReloading)
		require.NoError(t, err)
		currentState := StatusReloading // The state we expect to be marshaled

		jsonData, err := json.Marshal(fsm)
		require.NoError(t, err)

		// Expected JSON structure: {"state":"StatusReloading"}
		expectedJSON := `{"state":"` + currentState + `"}`
		assert.JSONEq(t, expectedJSON, string(jsonData))
	})

	t.Run("NewFromJSON - Success", func(t *testing.T) {
		originalState := StatusRunning
		fsmOrig, err := New(testHandler, StatusNew, TypicalTransitions)
		require.NoError(t, err)
		err = fsmOrig.Transition(StatusBooting) // Transition a few times
		require.NoError(t, err)
		err = fsmOrig.Transition(originalState)
		require.NoError(t, err)

		// Marshal the original FSM
		jsonData, err := json.Marshal(fsmOrig)
		require.NoError(t, err)

		// Restore using NewFromJSON
		fsmRestored, err := NewFromJSON(testHandler, jsonData, TypicalTransitions)
		require.NoError(t, err)
		require.NotNil(t, fsmRestored)

		// Verify the state of the restored FSM
		assert.Equal(t, originalState, fsmRestored.GetState())

		// Optional: Verify transitions still work on the restored machine
		err = fsmRestored.Transition(StatusReloading)
		assert.NoError(t, err)
		assert.Equal(t, StatusReloading, fsmRestored.GetState())
	})

	t.Run("NewFromJSON - Invalid JSON Data", func(t *testing.T) {
		invalidJSON := []byte(`{"state":`) // Malformed JSON

		fsmRestored, err := NewFromJSON(testHandler, invalidJSON, TypicalTransitions)
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		// Check if the error is related to JSON unmarshaling (it should be)
		assert.Contains(t, err.Error(), "failed to unmarshal FSM JSON data")
	})

	t.Run("NewFromJSON - Unknown State in JSON", func(t *testing.T) {
		unknownStateJSON := []byte(
			`{"state":"StatusUnknown"}`,
		) // Assume StatusUnknown is not in TypicalTransitions

		fsmRestored, err := NewFromJSON(testHandler, unknownStateJSON, TypicalTransitions)
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		// NewFromJSON calls New, which validates the initial state.
		// Expect ErrInvalidState wrapped in the NewFromJSON error message.
		assert.ErrorIs(t, err, ErrInvalidState)
		assert.Contains(
			t,
			err.Error(),
			"failed to initialize FSM with restored state 'StatusUnknown'",
		)
	})

	t.Run("NewFromJSON - Nil Transitions Config", func(t *testing.T) {
		// Marshal a valid state first
		fsmOrig, err := New(testHandler, StatusNew, TypicalTransitions)
		require.NoError(t, err)
		jsonData, err := json.Marshal(fsmOrig)
		require.NoError(t, err)

		// Attempt to restore with nil transitions
		fsmRestored, err := NewFromJSON(testHandler, jsonData, nil)
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		// NewFromJSON calls New, which returns ErrAvailableStateData for nil transitions.
		assert.ErrorIs(t, err, ErrAvailableStateData)
		assert.Contains(
			t,
			err.Error(),
			"failed to initialize FSM with restored state",
		) // Check wrapping
	})

	t.Run("NewFromJSON - Empty JSON Data", func(t *testing.T) {
		emptyJSON := []byte(`{}`) // Valid JSON, but missing "state" field

		fsmRestored, err := NewFromJSON(testHandler, emptyJSON, TypicalTransitions)
		// The unmarshal step will succeed, but pState.State will be "" (empty string)
		// The subsequent call to New("", TypicalTransitions) should fail validation.
		require.Error(t, err)
		assert.Nil(t, fsmRestored)
		assert.ErrorIs(t, err, ErrInvalidState) // New should return ErrInvalidState for ""
		assert.Contains(t, err.Error(), "failed to initialize FSM with restored state ''")
	})
}

func TestFSM_GetAllStates(t *testing.T) {
	t.Parallel()

	t.Run("GetAllStates with TypicalTransitions", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		states := fsm.GetAllStates()

		expectedStates := []string{
			StatusNew, StatusBooting, StatusRunning, StatusReloading,
			StatusStopping, StatusStopped, StatusError, StatusUnknown,
		}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, len(expectedStates))
	})

	t.Run("GetAllStates with custom transitions", func(t *testing.T) {
		customTransitions := TransitionsConfig{
			"StateA": {"StateB", "StateC"},
			"StateB": {"StateC"},
			"StateC": {"StateA"},
		}

		fsm, err := New(nil, "StateA", customTransitions)
		require.NoError(t, err)

		states := fsm.GetAllStates()
		expectedStates := []string{"StateA", "StateB", "StateC"}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, 3)
	})

	t.Run("GetAllStates with single state", func(t *testing.T) {
		singleStateTransitions := TransitionsConfig{
			"OnlyState": {},
		}

		fsm, err := New(nil, "OnlyState", singleStateTransitions)
		require.NoError(t, err)

		states := fsm.GetAllStates()
		expectedStates := []string{"OnlyState"}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, 1)
	})

	t.Run("GetAllStates returns copy not reference", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		states1 := fsm.GetAllStates()
		states2 := fsm.GetAllStates()

		assert.ElementsMatch(t, states1, states2)

		// Modify one slice to ensure they're independent
		states1[0] = "ModifiedState"
		assert.NotEqual(t, states1[0], states2[0])
	})
}
