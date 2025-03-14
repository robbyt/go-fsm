package fsm

import (
	"context"
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
	})

	t.Run("NewFSM with nil allowedTransitions", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, nil)
		assert.Nil(t, fsm)
		require.Error(t, err)
	})

	t.Run("GetState and SetState", func(t *testing.T) {
		fsm, err := New(nil, StatusNew, TypicalTransitions)
		require.NoError(t, err)

		assert.Equal(t, StatusNew, fsm.GetState())

		err = fsm.SetState(StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, StatusRunning, fsm.GetState())

		// Test setting an invalid state
		err = fsm.SetState("invalid_state")
		assert.ErrorIs(t, err, ErrInvalidState)
		assert.Equal(t, StatusRunning, fsm.GetState())
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
				toState:       StatusRunning,
				expectedErr:   ErrCurrentStateIncorrect,
				expectedState: StatusBooting,
			},
			{
				name:          "Invalid transition due to invalid state transition",
				initialState:  StatusNew,
				fromState:     StatusNew,
				toState:       StatusRunning,
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

	err = fsm.Transition("InvalidState")

	assert.ErrorIs(t, err, ErrInvalidStateTransition)
	assert.Equal(t, StatusNew, fsm.GetState())
}

func TestFSM_Transition_ModifiedAllowedTransitions(t *testing.T) {
	t.Parallel()

	fsm, err := New(nil, StatusNew, TypicalTransitions)
	require.NoError(t, err)

	// Remove all allowed transitions for the current state
	fsm.mutex.Lock()
	delete(fsm.transitionIndex, StatusNew)
	fsm.mutex.Unlock()

	t.Run("Transition with removed allowedTransitions", func(t *testing.T) {
		err := fsm.Transition(StatusBooting)
		assert.ErrorIs(t, err, ErrInvalidState)
		assert.Equal(t, StatusNew, fsm.GetState())
	})
}

func TestFSM_NoAllowedTransitions(t *testing.T) {
	// Create a small transition configuration with limited states
	smallestTransitions := TransitionsConfig{
		StatusNew:   {StatusError},
		StatusError: {},
	}
	fsm, err := New(nil, StatusNew, smallestTransitions)
	require.NoError(t, err)

	// Create a context with timeout to prevent test from hanging
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get state channel and read initial state
	listener := fsm.GetStateChan(ctx)
	initialState := <-listener
	assert.Equal(t, StatusNew, initialState, "First state should be the initial state")

	// Transition to StatusError
	err = fsm.Transition(StatusError)
	require.NoError(t, err)
	assert.Equal(t, StatusError, fsm.GetState())

	// Read the updated state from channel
	select {
	case updatedState := <-listener:
		assert.Equal(t, StatusError, updatedState, "Should receive state transition to StatusError")
	case <-ctx.Done():
		t.Fatal("Timed out waiting for state update")
	}

	// Attempt invalid transition - should fail
	err = fsm.Transition(StatusNew)
	require.ErrorIs(t, err, ErrInvalidStateTransition)
	assert.Equal(t, StatusError, fsm.GetState(), "State should not change after failed transition")

	// Verify no state update was sent
	select {
	case unexpectedState := <-listener:
		t.Errorf("No state transition expected, but received: %s", unexpectedState)
	case <-time.After(100 * time.Millisecond):
		// All good, no state update received
	}
}

// Removing unused function to fix linting warning

func TestFSM_RaceCondition_Broadcast(t *testing.T) {
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

	// Read initial state
	initialState := <-listener
	assert.Equal(t, StateA, initialState)

	// Perform a series of transitions
	stateSequence := []string{StateB, StateC, StateD, StateE, StateF, StateG}
	for _, state := range stateSequence {
		err := fsmMachine.Transition(state)
		assert.NoError(t, err)

		// Verify the state changed correctly
		assert.Equal(t, state, fsmMachine.GetState())

		// Read the state update from channel with timeout
		select {
		case receivedState := <-listener:
			assert.Equal(t, state, receivedState)
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Timed out waiting for state update to %s", state)
		}
	}

	// Final transition back to StateA
	err = fsmMachine.Transition(StateA)
	assert.NoError(t, err)
	assert.Equal(t, StateA, fsmMachine.GetState())

	select {
	case receivedState := <-listener:
		assert.Equal(t, StateA, receivedState)
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for final state update")
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
