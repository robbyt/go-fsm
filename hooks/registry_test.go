package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/robbyt/go-fsm/v2/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutePreTransitionHooks(t *testing.T) {
	t.Parallel()

	t.Run("Transition hook executes successfully", func(t *testing.T) {
		called := false
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-pre-hook-success",
			From: []string{"New"},
			To:   []string{"Booting"},
			Guard: func(ctx context.Context, from, to string) error {
				called = true
				return nil
			},
		})
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks(context.Background(), "New", "Booting")
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("Transition hook failure returns error", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-pre-hook-failure",
			From: []string{"New"},
			To:   []string{"Booting"},
			Guard: func(ctx context.Context, from, to string) error {
				return errors.New("action failed")
			},
		})
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks(context.Background(), "New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCallbackFailed)
	})
}

func TestExecutePostTransitionHooks(t *testing.T) {
	t.Parallel()

	t.Run("Post-transition hook executes", func(t *testing.T) {
		called := false
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-hook-executes",
			From: []string{"New"},
			To:   []string{"Booting"},
			Action: func(ctx context.Context, from, to string) {
				called = true
			},
		})
		require.NoError(t, err)

		reg.ExecutePostTransitionHooks(context.Background(), "New", "Booting")
		assert.True(t, called)
	})

	t.Run("Multiple hooks execute in FIFO order", func(t *testing.T) {
		var order []int
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-hook-fifo-1",
			From: []string{"New"},
			To:   []string{"Booting"},
			Action: func(ctx context.Context, from, to string) {
				order = append(order, 1)
			},
		})
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-hook-fifo-2",
			From: []string{"New"},
			To:   []string{"Booting"},
			Action: func(ctx context.Context, from, to string) {
				order = append(order, 2)
			},
		})
		require.NoError(t, err)

		reg.ExecutePostTransitionHooks(context.Background(), "New", "Booting")
		assert.Equal(t, []int{1, 2}, order)
	})
}

func TestPanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Panic in pre-transition hook is recovered and returned as error", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-pre-hook-panic",
			From: []string{"New"},
			To:   []string{"Booting"},
			Guard: func(ctx context.Context, from, to string) error {
				panic("transition panic")
			},
		})
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks(context.Background(), "New", "Booting")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrCallbackFailed)
		assert.Contains(t, err.Error(), "callback panicked")
	})

	t.Run("Panic in post-transition hook is recovered and logged", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-hook-panic",
			From: []string{"New"},
			To:   []string{"Booting"},
			Action: func(ctx context.Context, from, to string) {
				panic("hook panic")
			},
		})
		require.NoError(t, err)

		// Should not panic
		reg.ExecutePostTransitionHooks(context.Background(), "New", "Booting")
	})
}

func TestClear(t *testing.T) {
	t.Parallel()

	t.Run("Clear removes all callbacks", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-clear-pre-hook",
			From: []string{"New"},
			To:   []string{"Booting"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.NoError(t, err)
		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-clear-post-hook",
			From: []string{"New"},
			To:   []string{"Booting"},
			Action: func(ctx context.Context, from, to string) {
			},
		})
		require.NoError(t, err)

		reg.Clear()

		reg.mu.RLock()
		assert.Empty(t, reg.preTransitionHooksIndex)
		assert.Empty(t, reg.postTransitionHooksIndex)
		assert.Empty(t, reg.preWildcardHooks)
		assert.Empty(t, reg.postWildcardHooks)
		assert.Empty(t, reg.hooks)
		assert.Equal(t, 0, reg.nextSeq)
		reg.mu.RUnlock()
	})
}

func TestOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithLogger rejects nil logger", func(t *testing.T) {
		_, err := NewRegistry(WithLogger(nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "logger cannot be nil")
	})

	t.Run("WithLogHandler rejects nil handler", func(t *testing.T) {
		_, err := NewRegistry(WithLogHandler(nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "log handler cannot be nil")
	})

	t.Run("WithLogHandler creates logger", func(t *testing.T) {
		handler := slog.Default().Handler()
		reg, err := NewRegistry(WithLogHandler(handler))
		require.NoError(t, err)
		assert.NotNil(t, reg.logger)
	})

	t.Run("WithTransitions rejects nil transitions", func(t *testing.T) {
		_, err := NewRegistry(WithTransitions(nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transitions cannot be nil")
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

	t.Run("Wildcard pattern matches all states at runtime", func(t *testing.T) {
		called := make(map[string]bool)
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		// Register wildcard hook - evaluated at runtime, not expanded at registration
		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-wildcard-runtime-match",
			From: []string{"*"},
			To:   []string{"StatusError"},
			Guard: func(ctx context.Context, from, to string) error {
				called[from] = true
				return nil
			},
		})
		require.NoError(t, err)

		// Test transitions from different states - wildcard matches all at runtime
		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusNew", "StatusError")
		require.NoError(t, err)
		assert.True(t, called["StatusNew"])

		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusBooting", "StatusError")
		require.NoError(t, err)
		assert.True(t, called["StatusBooting"])
	})

	t.Run("Multiple concrete states", func(t *testing.T) {
		var executionOrder []string
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		// Register hook for multiple source states
		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-multiple-concrete-states",
			From: []string{"StatusNew", "StatusBooting"},
			To:   []string{"StatusError"},
			Guard: func(ctx context.Context, from, to string) error {
				executionOrder = append(executionOrder, from)
				return nil
			},
		})
		require.NoError(t, err)

		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusNew", "StatusError")
		require.NoError(t, err)
		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusBooting", "StatusError")
		require.NoError(t, err)

		assert.Equal(t, []string{"StatusNew", "StatusBooting"}, executionOrder)
	})

	t.Run("Invalid state returns error", func(t *testing.T) {
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-invalid-from-state",
			From: []string{"InvalidState"},
			To:   []string{"StatusError"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown state 'InvalidState'")
	})

	t.Run("Wildcard without state table returns error", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-wildcard-without-state-table",
			From: []string{"*"},
			To:   []string{"StatusError"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wildcard '*' cannot be used without state table")
	})

	t.Run("Wildcard in to position without state table returns error", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-wildcard-to-without-state-table",
			From: []string{"StatusNew"},
			To:   []string{"*"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wildcard '*' cannot be used without state table")
	})

	t.Run("Invalid to state returns error", func(t *testing.T) {
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-invalid-to-state",
			From: []string{"StatusNew"},
			To:   []string{"InvalidState"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown state 'InvalidState'")
	})

	t.Run("Wildcard to wildcard creates Cartesian product", func(t *testing.T) {
		executed := make(map[string]int)
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-wildcard-cartesian-product",
			From: []string{"*"},
			To:   []string{"*"},
			Guard: func(ctx context.Context, from, to string) error {
				key := fmt.Sprintf("%s->%s", from, to)
				executed[key]++
				return nil
			},
		})
		require.NoError(t, err)

		// Execute one transition to verify hook is registered
		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusNew", "StatusBooting")
		require.NoError(t, err)
		assert.Equal(t, 1, executed["StatusNew->StatusBooting"])
	})

	t.Run("Wildcard and concrete hooks maintain FIFO order", func(t *testing.T) {
		var order []string
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		// Register in order: concrete A, wildcard B, concrete C
		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "concrete-a",
			From: []string{"StatusNew"},
			To:   []string{"StatusBooting"},
			Guard: func(ctx context.Context, from, to string) error {
				order = append(order, "A")
				return nil
			},
		})
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "wildcard-b",
			From: []string{"*"},
			To:   []string{"StatusBooting"},
			Guard: func(ctx context.Context, from, to string) error {
				order = append(order, "B")
				return nil
			},
		})
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "concrete-c",
			From: []string{"StatusNew"},
			To:   []string{"StatusBooting"},
			Guard: func(ctx context.Context, from, to string) error {
				order = append(order, "C")
				return nil
			},
		})
		require.NoError(t, err)

		// Execute transition - should maintain registration order A -> B -> C
		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusNew", "StatusBooting")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C"}, order)
	})

	t.Run("RemoveHook removes wildcard hooks", func(t *testing.T) {
		var executed []string
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		// Register wildcard hook
		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-wildcard-remove",
			From: []string{"*"},
			To:   []string{"StatusError"},
			Guard: func(ctx context.Context, from, to string) error {
				executed = append(executed, "wildcard")
				return nil
			},
		})
		require.NoError(t, err)

		// Verify it executes
		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusNew", "StatusError")
		require.NoError(t, err)
		assert.Equal(t, []string{"wildcard"}, executed)

		// Remove the hook
		err = reg.RemoveHook("test-wildcard-remove")
		require.NoError(t, err)

		// Verify it no longer executes
		executed = nil
		err = reg.ExecutePreTransitionHooks(context.Background(), "StatusNew", "StatusError")
		require.NoError(t, err)
		assert.Empty(t, executed)
	})
}

func TestGetHooks(t *testing.T) {
	t.Parallel()

	t.Run("Empty registry returns no hooks", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		count := 0
		for range reg.GetHooks() {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("Single hook is returned", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-hook",
			From: []string{"A"},
			To:   []string{"B"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.NoError(t, err)

		count := 0
		for info := range reg.GetHooks() {
			count++
			assert.Equal(t, "test-hook", info.Name)
			assert.Equal(t, []string{"A"}, info.FromStates)
			assert.Equal(t, []string{"B"}, info.ToStates)
			assert.Equal(t, HookTypePre, info.Type)
		}
		assert.Equal(t, 1, count)
	})

	t.Run("Multiple hooks are returned", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "pre-hook",
			From: []string{"A"},
			To:   []string{"B"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.NoError(t, err)

		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "post-hook",
			From: []string{"C"},
			To:   []string{"D"},
			Action: func(ctx context.Context, from, to string) {
			},
		})
		require.NoError(t, err)

		hooks := make(map[string]HookInfo)
		for info := range reg.GetHooks() {
			hooks[info.Name] = info
		}

		assert.Len(t, hooks, 2)
		assert.Equal(t, HookTypePre, hooks["pre-hook"].Type)
		assert.Equal(t, HookTypePost, hooks["post-hook"].Type)
	})

	t.Run("Wildcard hooks are included", func(t *testing.T) {
		trans := transitions.MustNew(map[string][]string{
			"StatusNew":     {"StatusBooting"},
			"StatusBooting": {},
		})
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "wildcard-hook",
			From: []string{"*"},
			To:   []string{"StatusBooting"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.NoError(t, err)

		found := false
		for info := range reg.GetHooks() {
			if info.Name == "wildcard-hook" {
				found = true
				assert.Equal(t, []string{"*"}, info.FromStates)
				assert.Equal(t, []string{"StatusBooting"}, info.ToStates)
			}
		}
		assert.True(t, found, "wildcard hook should be returned")
	})

	t.Run("Returns defensive copies", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "defensive-test",
			From: []string{"A"},
			To:   []string{"B"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.NoError(t, err)

		// Mutate the returned slices
		for info := range reg.GetHooks() {
			info.FromStates[0] = "MUTATED"
			info.ToStates = append(info.ToStates, "EXTRA")
		}

		// Verify internal state is unchanged
		for info := range reg.GetHooks() {
			assert.Equal(t, []string{"A"}, info.FromStates)
			assert.Equal(t, []string{"B"}, info.ToStates)
		}
	})

	t.Run("Early termination works", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		for i := range 5 {
			err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
				Name: fmt.Sprintf("hook-%d", i),
				From: []string{"A"},
				To:   []string{"B"},
				Guard: func(ctx context.Context, from, to string) error {
					return nil
				},
			})
			require.NoError(t, err)
		}

		count := 0
		for range reg.GetHooks() {
			count++
			if count == 2 {
				break
			}
		}
		assert.Equal(t, 2, count)
	})

	t.Run("Hook metadata is accurate", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "multi-state-hook",
			From: []string{"A", "B", "C"},
			To:   []string{"D", "E"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.NoError(t, err)

		for info := range reg.GetHooks() {
			if info.Name == "multi-state-hook" {
				assert.Equal(t, []string{"A", "B", "C"}, info.FromStates)
				assert.Equal(t, []string{"D", "E"}, info.ToStates)
				assert.Equal(t, HookTypePre, info.Type)
			}
		}
	})
}

func TestRegisterHookValidation(t *testing.T) {
	t.Parallel()

	t.Run("RegisterPreTransitionHook rejects empty from list", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-empty-from-list",
			From: []string{},
			To:   []string{"StatusError"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from and to state lists cannot be empty")
	})

	t.Run("RegisterPreTransitionHook rejects empty to list", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-empty-to-list",
			From: []string{"StatusNew"},
			To:   []string{},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from and to state lists cannot be empty")
	})

	t.Run("RegisterPostTransitionHook rejects empty from list", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-empty-from-list",
			From: []string{},
			To:   []string{"StatusError"},
			Action: func(ctx context.Context, from, to string) {
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from and to state lists cannot be empty")
	})

	t.Run("RegisterPostTransitionHook rejects empty to list", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-empty-to-list",
			From: []string{"StatusNew"},
			To:   []string{},
			Action: func(ctx context.Context, from, to string) {
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from and to state lists cannot be empty")
	})

	t.Run("RegisterPostTransitionHook rejects wildcard without state table", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-wildcard-without-state-table",
			From: []string{"*"},
			To:   []string{"StatusError"},
			Action: func(ctx context.Context, from, to string) {
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wildcard '*' cannot be used without state table")
	})

	t.Run("RegisterPostTransitionHook with valid state succeeds", func(t *testing.T) {
		trans := transitions.MustNew(map[string][]string{
			"StatusNew":     {"StatusBooting"},
			"StatusBooting": {},
		})
		reg, err := NewRegistry(
			WithLogger(slog.Default()),
			WithTransitions(trans),
		)
		require.NoError(t, err)

		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-valid-state",
			From: []string{"StatusNew"},
			To:   []string{"StatusBooting"},
			Action: func(ctx context.Context, from, to string) {
			},
		})
		require.NoError(t, err)
	})

	t.Run("RegisterPreTransitionHook without transitions allows any state", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPreTransitionHook(PreTransitionHookConfig{
			Name: "test-pre-any-state-without-transitions",
			From: []string{"AnyFromState"},
			To:   []string{"AnyToState"},
			Guard: func(ctx context.Context, from, to string) error {
				return nil
			},
		})
		require.NoError(t, err)
	})

	t.Run("RegisterPostTransitionHook without transitions allows any state", func(t *testing.T) {
		reg, err := NewRegistry(WithLogger(slog.Default()))
		require.NoError(t, err)

		err = reg.RegisterPostTransitionHook(PostTransitionHookConfig{
			Name: "test-post-any-state-without-transitions",
			From: []string{"AnyFromState"},
			To:   []string{"AnyToState"},
			Action: func(ctx context.Context, from, to string) {
			},
		})
		require.NoError(t, err)
	})
}
