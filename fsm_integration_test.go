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

		reg, err := hooks.NewSynchronousCallbackRegistry(
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

		reg, err := hooks.NewSynchronousCallbackRegistry(
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

		reg, err := hooks.NewSynchronousCallbackRegistry(
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
		reg, err := hooks.NewSynchronousCallbackRegistry(
			hooks.WithLogger(slog.Default()),
			hooks.WithTransitions(transitions.Typical),
		)
		require.NoError(t, err)

		machine, err := fsm.New(transitions.StatusNew, transitions.Typical,
			fsm.WithCallbackRegistry(reg),
		)
		require.NoError(t, err)

		// Create standalone broadcast manager
		broadcastManager := broadcast.NewManager(slog.Default())

		// Manually register broadcast hook
		err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
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
		reg, err := hooks.NewSynchronousCallbackRegistry(
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
