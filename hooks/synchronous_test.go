package hooks

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/robbyt/go-fsm/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutePreTransitionHooks(t *testing.T) {
	t.Parallel()

	t.Run("Transition hook executes successfully", func(t *testing.T) {
		called := false
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) error {
			called = true
			return nil
		})
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks("New", "Booting")
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("Transition hook failure returns error", func(t *testing.T) {
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) error {
			return errors.New("action failed")
		})
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks("New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCallbackFailed)
	})
}

func TestExecutePostTransitionHooks(t *testing.T) {
	t.Parallel()

	t.Run("Post-transition hook executes", func(t *testing.T) {
		called := false
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) {
			called = true
		})
		require.NoError(t, err)

		reg.ExecutePostTransitionHooks("New", "Booting")
		assert.True(t, called)
	})

	t.Run("Multiple hooks execute in FIFO order", func(t *testing.T) {
		var order []int
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) {
			order = append(order, 1)
		})
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) {
			order = append(order, 2)
		})
		require.NoError(t, err)

		reg.ExecutePostTransitionHooks("New", "Booting")
		assert.Equal(t, []int{1, 2}, order)
	})
}

func TestPanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Panic in pre-transition hook is recovered and returned as error", func(t *testing.T) {
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) error {
			panic("transition panic")
		})
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks("New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCallbackFailed)
		assert.Contains(t, err.Error(), "callback panicked")
	})

	t.Run("Panic in post-transition hook is recovered and logged", func(t *testing.T) {
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) {
			panic("hook panic")
		})
		require.NoError(t, err)

		// Should not panic
		reg.ExecutePostTransitionHooks("New", "Booting")
	})
}

func TestClear(t *testing.T) {
	t.Parallel()

	t.Run("Clear removes all callbacks", func(t *testing.T) {
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) error {
			return nil
		})
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook([]string{"New"}, []string{"Booting"}, func(ctx context.Context, from, to string) {
		})
		require.NoError(t, err)

		reg.Clear()

		reg.mu.RLock()
		assert.Empty(t, reg.preTransitionHooks)
		assert.Empty(t, reg.postTransition)
		reg.mu.RUnlock()
	})
}

func TestPatternExpansion(t *testing.T) {
	t.Parallel()

	trans := transitions.MustNew(map[string][]string{
		"StatusNew":     {"StatusBooting", "StatusError"},
		"StatusBooting": {"StatusRunning", "StatusError"},
		"StatusRunning": {"StatusStopped", "StatusError"},
		"StatusError":   {"StatusNew"},
		"StatusStopped": {},
	})

	t.Run("Wildcard pattern expands to all states", func(t *testing.T) {
		called := make(map[string]bool)
		reg, err := NewSynchronousCallbackRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		// Register hook for all transitions to transitions.StatusError
		err = reg.RegisterPreTransitionHook([]string{"*"}, []string{"StatusError"}, func(ctx context.Context, from, to string) error {
			called[from] = true
			return nil
		})
		require.NoError(t, err)

		// Test transitions from different states
		err = reg.ExecutePreTransitionHooks("StatusNew", "StatusError")
		require.NoError(t, err)
		assert.True(t, called["StatusNew"])

		err = reg.ExecutePreTransitionHooks("StatusBooting", "StatusError")
		require.NoError(t, err)
		assert.True(t, called["StatusBooting"])
	})

	t.Run("Multiple concrete states", func(t *testing.T) {
		var executionOrder []string
		reg, err := NewSynchronousCallbackRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		// Register hook for multiple source states
		err = reg.RegisterPreTransitionHook(
			[]string{"StatusNew", "StatusBooting"},
			[]string{"StatusError"},
			func(ctx context.Context, from, to string) error {
				executionOrder = append(executionOrder, from)
				return nil
			},
		)
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks("StatusNew", "StatusError")
		require.NoError(t, err)
		err = reg.ExecutePreTransitionHooks("StatusBooting", "StatusError")
		require.NoError(t, err)

		assert.Equal(t, []string{"StatusNew", "StatusBooting"}, executionOrder)
	})

	t.Run("Invalid state returns error", func(t *testing.T) {
		reg, err := NewSynchronousCallbackRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(
			[]string{"InvalidState"},
			[]string{"StatusError"},
			func(ctx context.Context, from, to string) error {
				return nil
			},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown state 'InvalidState'")
	})

	t.Run("Wildcard without state table returns error", func(t *testing.T) {
		reg, err := NewSynchronousCallbackRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(
			[]string{"*"},
			[]string{"StatusError"},
			func(ctx context.Context, from, to string) error {
				return nil
			},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wildcard '*' cannot be used without state table")
	})
}
