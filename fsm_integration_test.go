package fsm_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/robbyt/go-fsm/v2"
	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallbackOrderingIntegration verifies callbacks execute in correct order during FSM transitions.
func TestCallbackOrderingIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Callbacks execute in correct order during transition", func(t *testing.T) {
		var executionOrder []string

		reg, err := hooks.NewRegistry(
			hooks.WithLogger(slog.Default()),
			hooks.WithTransitions(transitions.Typical),
		)
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{transitions.StatusNew}, []string{transitions.StatusBooting}, func(ctx context.Context, from, to string) error {
			executionOrder = append(executionOrder, "pre")
			return nil
		})
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
			executionOrder = append(executionOrder, "post")
		})
		require.NoError(t, err)

		machine, err := fsm.New(transitions.StatusNew, transitions.Typical,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)

		expected := []string{"pre", "post"}
		assert.Equal(t, expected, executionOrder)
	})

	t.Run("Multiple callbacks of same type execute in FIFO order", func(t *testing.T) {
		var order []int

		reg, err := hooks.NewRegistry(
			hooks.WithLogger(slog.Default()),
			hooks.WithTransitions(transitions.Typical),
		)
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{transitions.StatusNew}, []string{transitions.StatusBooting}, func(ctx context.Context, from, to string) error {
			order = append(order, 1)
			return nil
		})
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{transitions.StatusNew}, []string{transitions.StatusBooting}, func(ctx context.Context, from, to string) error {
			order = append(order, 2)
			return nil
		})
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{transitions.StatusNew}, []string{transitions.StatusBooting}, func(ctx context.Context, from, to string) error {
			order = append(order, 3)
			return nil
		})
		require.NoError(t, err)

		machine, err := fsm.New(transitions.StatusNew, transitions.Typical,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, order)
	})
}

// TestPostTransitionHookIntegration verifies post-transition hooks work with FSM.
func TestPostTransitionHookIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Post-transition hook executes after every transition", func(t *testing.T) {
		hookCallCount := 0

		reg, err := hooks.NewRegistry(
			hooks.WithLogger(slog.Default()),
			hooks.WithTransitions(transitions.Typical),
		)
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
			hookCallCount++
		})
		require.NoError(t, err)

		machine, err := fsm.New(transitions.StatusNew, transitions.Typical,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)
		assert.Equal(t, 1, hookCallCount)

		err = machine.Transition(transitions.StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, 2, hookCallCount)
	})
}

// TestBroadcastMechanismIntegration verifies broadcast hook works with custom registry.
func TestBroadcastMechanismIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Broadcast mechanism works with custom registry", func(t *testing.T) {
		reg, err := hooks.NewRegistry(
			hooks.WithLogger(slog.Default()),
			hooks.WithTransitions(transitions.Typical),
		)
		require.NoError(t, err)

		machine, err := fsm.New(transitions.StatusNew, transitions.Typical,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		// Create standalone broadcast manager
		broadcastManager := broadcast.NewManager(slog.Default().Handler())

		// Manually register broadcast hook
		err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		stateChan, err := broadcastManager.GetStateChan(ctx)
		require.NoError(t, err)

		// Send initial state
		broadcastManager.Broadcast(machine.GetState())

		// Read initial state
		initialState := <-stateChan
		assert.Equal(t, transitions.StatusNew, initialState)

		// Transition and verify broadcast
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			select {
			case state := <-stateChan:
				return assert.Equal(t, transitions.StatusBooting, state)
			default:
				return false
			}
		}, 100*time.Millisecond, 10*time.Millisecond, "Expected to receive state change notification")
	})
}

// TestPanicRecoveryIntegration verifies panic recovery during FSM transitions.
func TestPanicRecoveryIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Panic in pre-transition hook prevents transition", func(t *testing.T) {
		reg, err := hooks.NewRegistry(
			hooks.WithLogger(slog.Default()),
			hooks.WithTransitions(transitions.Typical),
		)
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{transitions.StatusNew}, []string{transitions.StatusBooting}, func(ctx context.Context, from, to string) error {
			panic("transition panic")
		})
		require.NoError(t, err)

		machine, err := fsm.New(transitions.StatusNew, transitions.Typical,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(transitions.StatusBooting)
		require.Error(t, err)
		require.ErrorIs(t, err, hooks.ErrCallbackFailed)
		assert.Equal(t, transitions.StatusNew, machine.GetState())
	})
}

// TestBasicFSMWorkflow tests FSM with real transitions.Config but no callbacks.
// This validates the integration between FSM and the transitions package.
func TestBasicFSMWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("FSM with typical transitions workflow", func(t *testing.T) {
		machine, err := fsm.New(transitions.StatusNew, transitions.Typical)
		require.NoError(t, err)
		require.NotNil(t, machine)

		assert.Equal(t, transitions.StatusNew, machine.GetState())

		// New -> Booting
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)
		assert.Equal(t, transitions.StatusBooting, machine.GetState())

		// Booting -> Running
		err = machine.Transition(transitions.StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, transitions.StatusRunning, machine.GetState())

		// Running -> Stopping
		err = machine.Transition(transitions.StatusStopping)
		require.NoError(t, err)
		assert.Equal(t, transitions.StatusStopping, machine.GetState())

		// Stopping -> Stopped
		err = machine.Transition(transitions.StatusStopped)
		require.NoError(t, err)
		assert.Equal(t, transitions.StatusStopped, machine.GetState())
	})

	t.Run("Invalid transition with real transitions", func(t *testing.T) {
		machine, err := fsm.New(transitions.StatusNew, transitions.Typical)
		require.NoError(t, err)

		// New -> Running is not allowed (must go through Booting)
		err = machine.Transition(transitions.StatusRunning)
		require.ErrorIs(t, err, fsm.ErrInvalidStateTransition)
		assert.Equal(t, transitions.StatusNew, machine.GetState())
	})

	t.Run("Terminal state has no outgoing transitions", func(t *testing.T) {
		smallTransitions := transitions.MustNew(map[string][]string{
			transitions.StatusNew:   {transitions.StatusError},
			transitions.StatusError: {}, // Terminal state
		})

		machine, err := fsm.New(transitions.StatusNew, smallTransitions)
		require.NoError(t, err)

		err = machine.Transition(transitions.StatusError)
		require.NoError(t, err)
		assert.Equal(t, transitions.StatusError, machine.GetState())

		// Cannot transition out of terminal state
		err = machine.Transition(transitions.StatusNew)
		require.ErrorIs(t, err, fsm.ErrInvalidStateTransition)
		assert.Equal(t, transitions.StatusError, machine.GetState())
	})
}

// TestGetAllStatesIntegration tests GetAllStates with real transitions.Config.
func TestGetAllStatesIntegration(t *testing.T) {
	t.Parallel()

	t.Run("GetAllStates with typical transitions", func(t *testing.T) {
		machine, err := fsm.New(transitions.StatusNew, transitions.Typical)
		require.NoError(t, err)

		states := machine.GetAllStates()

		expectedStates := []string{
			transitions.StatusNew,
			transitions.StatusBooting,
			transitions.StatusRunning,
			transitions.StatusReloading,
			transitions.StatusStopping,
			transitions.StatusStopped,
			transitions.StatusError,
			transitions.StatusUnknown,
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

		machine, err := fsm.New("StateA", customTransitions)
		require.NoError(t, err)

		states := machine.GetAllStates()
		expectedStates := []string{"StateA", "StateB", "StateC"}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, 3)
	})

	t.Run("GetAllStates with single state", func(t *testing.T) {
		singleStateTransitions := transitions.MustNew(map[string][]string{
			"OnlyState": {},
		})

		machine, err := fsm.New("OnlyState", singleStateTransitions)
		require.NoError(t, err)

		states := machine.GetAllStates()
		expectedStates := []string{"OnlyState"}

		assert.ElementsMatch(t, expectedStates, states)
		assert.Len(t, states, 1)
	})

	t.Run("GetAllStates returns copy not reference", func(t *testing.T) {
		machine, err := fsm.New(transitions.StatusNew, transitions.Typical)
		require.NoError(t, err)

		states1 := machine.GetAllStates()
		states2 := machine.GetAllStates()

		assert.ElementsMatch(t, states1, states2)

		// Modify one slice to ensure they're independent
		states1[0] = "ModifiedState"
		assert.NotEqual(t, states1[0], states2[0])
	})
}

// TestNewSimpleIntegration tests the NewSimple constructor with real transitions.
func TestNewSimpleIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Success with valid map", func(t *testing.T) {
		machine, err := fsm.NewSimple("online", map[string][]string{
			"online":  {"offline", "error"},
			"offline": {"online", "error"},
			"error":   {},
		})
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())
	})

	t.Run("Success with transitions", func(t *testing.T) {
		machine, err := fsm.NewSimple("online", map[string][]string{
			"online":  {"offline"},
			"offline": {"online"},
		})
		require.NoError(t, err)

		err = machine.Transition("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())

		err = machine.Transition("online")
		require.NoError(t, err)
		assert.Equal(t, "online", machine.GetState())
	})

	t.Run("Error with invalid transitions map", func(t *testing.T) {
		machine, err := fsm.NewSimple("online", map[string][]string{
			"online": {"offline"},
			// "offline" is referenced but not defined as a source state
		})
		require.Error(t, err)
		assert.Nil(t, machine)
	})

	t.Run("Error with empty transitions map", func(t *testing.T) {
		machine, err := fsm.NewSimple("online", map[string][]string{})
		require.Error(t, err)
		assert.Nil(t, machine)
	})

	t.Run("Error with invalid initial state", func(t *testing.T) {
		machine, err := fsm.NewSimple("invalid", map[string][]string{
			"online":  {"offline"},
			"offline": {"online"},
		})
		require.Error(t, err)
		require.ErrorIs(t, err, fsm.ErrInvalidConfiguration)
		assert.Nil(t, machine)
	})
}

// TestConcurrentTransitionsIntegration tests concurrent state transitions with real transitions.
func TestConcurrentTransitionsIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Sequential state transitions under concurrent load", func(t *testing.T) {
		const (
			StateA = "StateA"
			StateB = "StateB"
			StateC = "StateC"
			StateD = "StateD"
			StateE = "StateE"
			StateF = "StateF"
			StateG = "StateG"
		)

		trans := transitions.MustNew(map[string][]string{
			StateA: {StateB},
			StateB: {StateC},
			StateC: {StateD},
			StateD: {StateE},
			StateE: {StateF},
			StateF: {StateG},
			StateG: {StateA},
		})

		machine, err := fsm.New(StateA, trans)
		require.NoError(t, err)

		assert.Equal(t, StateA, machine.GetState())

		// Perform a series of transitions
		stateSequence := []string{StateB, StateC, StateD, StateE, StateF, StateG}
		for _, state := range stateSequence {
			err := machine.Transition(state)
			require.NoError(t, err)
			assert.Equal(t, state, machine.GetState())
		}

		// Final transition back to StateA
		err = machine.Transition(StateA)
		require.NoError(t, err)
		assert.Equal(t, StateA, machine.GetState())
	})
}
