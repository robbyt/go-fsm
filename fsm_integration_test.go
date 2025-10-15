package fsm_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/robbyt/go-fsm"
	"github.com/robbyt/go-fsm/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGuardIntegration verifies guards work correctly with FSM transitions.
func TestGuardIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Guard allows FSM transition", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard(fsm.StatusNew, fsm.StatusBooting, func(ctx context.Context, from, to string) error {
			return nil
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)
		assert.Equal(t, fsm.StatusBooting, machine.GetState())
	})

	t.Run("Guard rejects FSM transition", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard(fsm.StatusNew, fsm.StatusBooting, func(ctx context.Context, from, to string) error {
			return errors.New("not ready")
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.Error(t, err)
		require.ErrorIs(t, err, hooks.ErrGuardRejected)
		assert.Equal(t, fsm.StatusNew, machine.GetState(), "State should remain unchanged")
	})

	t.Run("Multiple guards all execute before transition", func(t *testing.T) {
		guard1Called := false
		guard2Called := false

		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard(fsm.StatusNew, fsm.StatusBooting, func(ctx context.Context, from, to string) error {
			guard1Called = true
			return nil
		})
		reg.RegisterGuard(fsm.StatusNew, fsm.StatusBooting, func(ctx context.Context, from, to string) error {
			guard2Called = true
			return errors.New("second guard rejects")
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.Error(t, err)
		assert.True(t, guard1Called)
		assert.True(t, guard2Called)
		assert.Equal(t, fsm.StatusNew, machine.GetState())
	})
}

// TestExitActionIntegration verifies exit actions work correctly with FSM transitions.
func TestExitActionIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Exit action executes during FSM transition", func(t *testing.T) {
		exitCalled := false

		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction(fsm.StatusNew, func(ctx context.Context, from, to string) error {
			exitCalled = true
			assert.Equal(t, fsm.StatusNew, from)
			assert.Equal(t, fsm.StatusBooting, to)
			return nil
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)
		assert.True(t, exitCalled)
	})

	t.Run("Exit action failure prevents FSM transition", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction(fsm.StatusNew, func(ctx context.Context, from, to string) error {
			return errors.New("cannot exit")
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.Error(t, err)
		require.ErrorIs(t, err, hooks.ErrCallbackFailed)
		assert.Equal(t, fsm.StatusNew, machine.GetState(), "State should remain unchanged")
	})
}

// TestEntryActionIntegration verifies entry actions work correctly with FSM transitions.
func TestEntryActionIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Entry action executes after FSM state update", func(t *testing.T) {
		var stateAtEntry string

		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterEntryAction(fsm.StatusBooting, func(ctx context.Context, from, to string) {
			// Entry action runs after state is updated
			stateAtEntry = to
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)
		assert.Equal(t, fsm.StatusBooting, stateAtEntry)
		assert.Equal(t, fsm.StatusBooting, machine.GetState())
	})

	t.Run("Entry action panic does not prevent transition", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterEntryAction(fsm.StatusBooting, func(ctx context.Context, from, to string) {
			panic("entry panic")
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err, "Transition should succeed despite entry action panic")
		assert.Equal(t, fsm.StatusBooting, machine.GetState())
	})
}

// TestPatternMatchingIntegration verifies pattern matching works with real FSM states.
func TestPatternMatchingIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Pattern matches multiple FSM states", func(t *testing.T) {
		var matchedStates []string

		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		err := reg.RegisterEntryActionPattern(".*ing$", func(ctx context.Context, from, to string) {
			matchedStates = append(matchedStates, to)
		}, fsm.TypicalTransitions.GetAllStates())
		require.NoError(t, err)

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)
		err = machine.Transition(fsm.StatusRunning)
		require.NoError(t, err)
		err = machine.Transition(fsm.StatusStopping)
		require.NoError(t, err)

		assert.Equal(t, []string{fsm.StatusBooting, fsm.StatusRunning, fsm.StatusStopping}, matchedStates)
	})

	t.Run("Invalid pattern returns error during registration", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		err := reg.RegisterEntryActionPattern("NonExistentState", func(ctx context.Context, from, to string) {
		}, fsm.TypicalTransitions.GetAllStates())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "matches no states")
	})
}

// TestCallbackOrderingIntegration verifies callbacks execute in correct order during FSM transitions.
func TestCallbackOrderingIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Callbacks execute in correct order during transition", func(t *testing.T) {
		var executionOrder []string

		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard(fsm.StatusNew, fsm.StatusBooting, func(ctx context.Context, from, to string) error {
			executionOrder = append(executionOrder, "guard")
			return nil
		})
		reg.RegisterExitAction(fsm.StatusNew, func(ctx context.Context, from, to string) error {
			executionOrder = append(executionOrder, "exit")
			return nil
		})
		reg.RegisterTransitionAction(fsm.StatusNew, fsm.StatusBooting, func(ctx context.Context, from, to string) error {
			executionOrder = append(executionOrder, "transition")
			return nil
		})
		reg.RegisterEntryAction(fsm.StatusBooting, func(ctx context.Context, from, to string) {
			executionOrder = append(executionOrder, "entry")
		})
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			executionOrder = append(executionOrder, "post")
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)

		expected := []string{"guard", "exit", "transition", "entry", "post"}
		assert.Equal(t, expected, executionOrder)
	})

	t.Run("Multiple callbacks of same type execute in FIFO order", func(t *testing.T) {
		var order []int

		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction(fsm.StatusNew, func(ctx context.Context, from, to string) error {
			order = append(order, 1)
			return nil
		})
		reg.RegisterExitAction(fsm.StatusNew, func(ctx context.Context, from, to string) error {
			order = append(order, 2)
			return nil
		})
		reg.RegisterExitAction(fsm.StatusNew, func(ctx context.Context, from, to string) error {
			order = append(order, 3)
			return nil
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, order)
	})
}

// TestPostTransitionHookIntegration verifies post-transition hooks work with FSM.
func TestPostTransitionHookIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Post-transition hook executes after every transition", func(t *testing.T) {
		hookCallCount := 0

		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			hookCallCount++
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)
		assert.Equal(t, 1, hookCallCount)

		err = machine.Transition(fsm.StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, 2, hookCallCount)
	})
}

// TestBroadcastMechanismIntegration verifies broadcast hook works with custom registry.
func TestBroadcastMechanismIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Broadcast mechanism works with custom registry", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		// Manually register broadcast hook
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			machine.Broadcast.Broadcast(to)
		})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		stateChan := machine.GetStateChan(ctx)

		// Read initial state
		initialState := <-stateChan
		assert.Equal(t, fsm.StatusNew, initialState)

		// Transition and verify broadcast
		err = machine.Transition(fsm.StatusBooting)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			select {
			case state := <-stateChan:
				return assert.Equal(t, fsm.StatusBooting, state)
			default:
				return false
			}
		}, 100*time.Millisecond, 10*time.Millisecond, "Expected to receive state change notification")
	})
}

// TestPanicRecoveryIntegration verifies panic recovery during FSM transitions.
func TestPanicRecoveryIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Panic in guard prevents transition", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard(fsm.StatusNew, fsm.StatusBooting, func(ctx context.Context, from, to string) error {
			panic("guard panic")
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.Error(t, err)
		require.ErrorIs(t, err, hooks.ErrGuardRejected)
		assert.Contains(t, err.Error(), "callback panicked")
		assert.Equal(t, fsm.StatusNew, machine.GetState())
	})

	t.Run("Panic in exit action prevents transition", func(t *testing.T) {
		reg := hooks.NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction(fsm.StatusNew, func(ctx context.Context, from, to string) error {
			panic("exit panic")
		})

		machine, err := fsm.New(nil, fsm.StatusNew, fsm.TypicalTransitions,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		err = machine.Transition(fsm.StatusBooting)
		require.Error(t, err)
		require.ErrorIs(t, err, hooks.ErrCallbackFailed)
		assert.Equal(t, fsm.StatusNew, machine.GetState())
	})
}
