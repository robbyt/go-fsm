package hooks

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteGuards(t *testing.T) {
	t.Parallel()

	t.Run("Guard allows transition", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			return nil
		})

		err := reg.ExecuteGuards("New", "Booting")
		require.NoError(t, err)
	})

	t.Run("Guard rejects transition", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			return errors.New("not ready")
		})

		err := reg.ExecuteGuards("New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrGuardRejected)
	})

	t.Run("Multiple guards execute in FIFO order", func(t *testing.T) {
		var order []int
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			order = append(order, 1)
			return nil
		})
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			order = append(order, 2)
			return nil
		})
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			order = append(order, 3)
			return nil
		})

		err := reg.ExecuteGuards("New", "Booting")
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, order)
	})

	t.Run("Second guard rejection stops execution", func(t *testing.T) {
		guard1Called := false
		guard2Called := false
		guard3Called := false

		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			guard1Called = true
			return nil
		})
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			guard2Called = true
			return errors.New("rejected")
		})
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			guard3Called = true
			return nil
		})

		err := reg.ExecuteGuards("New", "Booting")
		require.Error(t, err)
		assert.True(t, guard1Called)
		assert.True(t, guard2Called)
		assert.False(t, guard3Called, "Third guard should not execute after second rejects")
	})
}

func TestExecuteExitActions(t *testing.T) {
	t.Parallel()

	t.Run("Exit action executes successfully", func(t *testing.T) {
		called := false
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction("New", func(ctx context.Context, from, to string) error {
			called = true
			assert.Equal(t, "New", from)
			assert.Equal(t, "Booting", to)
			return nil
		})

		err := reg.ExecuteExitActions("New", "Booting")
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("Exit action failure returns error", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction("New", func(ctx context.Context, from, to string) error {
			return errors.New("cannot exit")
		})

		err := reg.ExecuteExitActions("New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCallbackFailed)
	})

	t.Run("Multiple exit actions execute in FIFO order", func(t *testing.T) {
		var order []int
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction("New", func(ctx context.Context, from, to string) error {
			order = append(order, 1)
			return nil
		})
		reg.RegisterExitAction("New", func(ctx context.Context, from, to string) error {
			order = append(order, 2)
			return nil
		})

		err := reg.ExecuteExitActions("New", "Booting")
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2}, order)
	})
}

func TestExecuteTransitionActions(t *testing.T) {
	t.Parallel()

	t.Run("Transition action executes successfully", func(t *testing.T) {
		called := false
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterTransitionAction("New", "Booting", func(ctx context.Context, from, to string) error {
			called = true
			return nil
		})

		err := reg.ExecuteTransitionActions("New", "Booting")
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("Transition action failure returns error", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterTransitionAction("New", "Booting", func(ctx context.Context, from, to string) error {
			return errors.New("action failed")
		})

		err := reg.ExecuteTransitionActions("New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCallbackFailed)
	})
}

func TestExecuteEntryActions(t *testing.T) {
	t.Parallel()

	t.Run("Entry action executes successfully", func(t *testing.T) {
		called := false
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterEntryAction("Booting", func(ctx context.Context, from, to string) {
			called = true
			assert.Equal(t, "New", from)
			assert.Equal(t, "Booting", to)
		})

		reg.ExecuteEntryActions("New", "Booting")
		assert.True(t, called)
	})

	t.Run("Multiple entry actions execute in FIFO order", func(t *testing.T) {
		var order []int
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterEntryAction("Booting", func(ctx context.Context, from, to string) {
			order = append(order, 1)
		})
		reg.RegisterEntryAction("Booting", func(ctx context.Context, from, to string) {
			order = append(order, 2)
		})

		reg.ExecuteEntryActions("New", "Booting")
		assert.Equal(t, []int{1, 2}, order)
	})
}

func TestExecutePostTransitionHooks(t *testing.T) {
	t.Parallel()

	t.Run("Post-transition hook executes", func(t *testing.T) {
		called := false
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			called = true
		})

		reg.ExecutePostTransitionHooks("New", "Booting")
		assert.True(t, called)
	})

	t.Run("Multiple hooks execute in FIFO order", func(t *testing.T) {
		var order []int
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			order = append(order, 1)
		})
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			order = append(order, 2)
		})

		reg.ExecutePostTransitionHooks("New", "Booting")
		assert.Equal(t, []int{1, 2}, order)
	})
}

func TestPanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Panic in guard is recovered and returned as error", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			panic("guard panic")
		})

		err := reg.ExecuteGuards("New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrGuardRejected)
		assert.Contains(t, err.Error(), "callback panicked")
	})

	t.Run("Panic in exit action is recovered and returned as error", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterExitAction("New", func(ctx context.Context, from, to string) error {
			panic("exit panic")
		})

		err := reg.ExecuteExitActions("New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCallbackFailed)
		assert.Contains(t, err.Error(), "callback panicked")
	})

	t.Run("Panic in entry action is recovered and logged", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterEntryAction("Booting", func(ctx context.Context, from, to string) {
			panic("entry panic")
		})

		// Should not panic
		reg.ExecuteEntryActions("New", "Booting")
	})

	t.Run("Panic in post-transition hook is recovered and logged", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
			panic("hook panic")
		})

		// Should not panic
		reg.ExecutePostTransitionHooks("New", "Booting")
	})
}

func TestRegisterEntryActionPattern(t *testing.T) {
	t.Parallel()

	t.Run("Exact state name match", func(t *testing.T) {
		called := false
		allStates := []string{"New", "Booting", "Running"}
		reg := NewSynchronousCallbackRegistry(slog.Default())

		err := reg.RegisterEntryActionPattern("Booting", func(ctx context.Context, from, to string) {
			called = true
		}, allStates)
		require.NoError(t, err)

		reg.ExecuteEntryActions("New", "Booting")
		assert.True(t, called)
	})

	t.Run("Regex pattern matches multiple states", func(t *testing.T) {
		allStates := []string{"New", "Booting", "Running", "Stopping", "Stopped"}
		reg := NewSynchronousCallbackRegistry(slog.Default())

		err := reg.RegisterEntryActionPattern(".*ing$", func(ctx context.Context, from, to string) {
		}, allStates)
		require.NoError(t, err)

		// Verify actions registered for matching states
		reg.mu.RLock()
		assert.NotEmpty(t, reg.entryActions["Booting"])
		assert.NotEmpty(t, reg.entryActions["Running"])
		assert.NotEmpty(t, reg.entryActions["Stopping"])
		assert.Empty(t, reg.entryActions["New"])
		assert.Empty(t, reg.entryActions["Stopped"])
		reg.mu.RUnlock()
	})

	t.Run("Invalid pattern returns error", func(t *testing.T) {
		allStates := []string{"New", "Booting"}
		reg := NewSynchronousCallbackRegistry(slog.Default())

		err := reg.RegisterEntryActionPattern("NonExistent", func(ctx context.Context, from, to string) {
		}, allStates)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "matches no states")
	})

	t.Run("Invalid regex returns error", func(t *testing.T) {
		allStates := []string{"New", "Booting"}
		reg := NewSynchronousCallbackRegistry(slog.Default())

		err := reg.RegisterEntryActionPattern("[invalid(", func(ctx context.Context, from, to string) {
		}, allStates)
		require.Error(t, err)
	})
}

func TestRegisterExitActionPattern(t *testing.T) {
	t.Parallel()

	t.Run("Pattern registers exit actions for matching states", func(t *testing.T) {
		allStates := []string{"New", "Booting", "Running"}
		reg := NewSynchronousCallbackRegistry(slog.Default())

		err := reg.RegisterExitActionPattern(".*ing$", func(ctx context.Context, from, to string) error {
			return nil
		}, allStates)
		require.NoError(t, err)

		reg.mu.RLock()
		assert.NotEmpty(t, reg.exitActions["Booting"])
		assert.NotEmpty(t, reg.exitActions["Running"])
		assert.Empty(t, reg.exitActions["New"])
		reg.mu.RUnlock()
	})
}

func TestResolveStatePattern(t *testing.T) {
	t.Parallel()

	allStates := []string{"New", "Booting", "Running", "Stopping", "Stopped"}

	t.Run("Exact match", func(t *testing.T) {
		result := ResolveStatePattern("Booting", allStates)
		assert.Equal(t, []string{"Booting"}, result)
	})

	t.Run("Regex match", func(t *testing.T) {
		result := ResolveStatePattern(".*ing$", allStates)
		assert.ElementsMatch(t, []string{"Booting", "Running", "Stopping"}, result)
	})

	t.Run("Wildcard match", func(t *testing.T) {
		result := ResolveStatePattern(".*", allStates)
		assert.ElementsMatch(t, allStates, result)
	})

	t.Run("No match", func(t *testing.T) {
		result := ResolveStatePattern("NonExistent", allStates)
		assert.Nil(t, result)
	})

	t.Run("Invalid regex", func(t *testing.T) {
		result := ResolveStatePattern("[invalid(", allStates)
		assert.Nil(t, result)
	})
}

func TestClear(t *testing.T) {
	t.Parallel()

	t.Run("Clear removes all callbacks", func(t *testing.T) {
		reg := NewSynchronousCallbackRegistry(slog.Default())

		reg.RegisterGuard("New", "Booting", func(ctx context.Context, from, to string) error {
			return nil
		})
		reg.RegisterExitAction("New", func(ctx context.Context, from, to string) error {
			return nil
		})
		reg.RegisterEntryAction("Booting", func(ctx context.Context, from, to string) {
		})
		reg.RegisterPostTransitionHook(func(ctx context.Context, from, to string) {
		})

		reg.Clear()

		reg.mu.RLock()
		assert.Empty(t, reg.guards)
		assert.Empty(t, reg.exitActions)
		assert.Empty(t, reg.entryActions)
		assert.Empty(t, reg.postTransition)
		reg.mu.RUnlock()
	})
}
