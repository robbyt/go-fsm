package fsm

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/hooks/broadcast"
	"github.com/robbyt/go-fsm/v2/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadme_QuickStartExample tests the complete Quick Start example from README.md
func TestReadme_QuickStartExample(t *testing.T) {
	t.Parallel()

	logger := slog.Default()

	// Create a new FSM with an initial state and a map of allowed transitions
	machine, err := NewSimple("new", map[string][]string{
		"new":     {"booting"},
		"booting": {"running"},
		"running": {"stopped", "error"},
		"stopped": {"new"},
		"error":   {}, // terminal state
	}, WithLogger(logger))
	require.NoError(t, err)
	assert.Equal(t, "new", machine.GetState())

	// Transition through a series of states
	states := []string{"booting", "running", "stopped"}
	for _, state := range states {
		if err := machine.Transition(state); err != nil {
			t.Fatalf("transition failed: %v", err)
		}
		assert.Equal(t, state, machine.GetState())
	}

	// This transition is not allowed and will fail
	err = machine.Transition("running") // Can't go from "stopped" to "running"
	require.Error(t, err)
	assert.Equal(t, "stopped", machine.GetState())
}

// TestReadme_CustomStatesAndTransitions tests the custom states example from README.md
func TestReadme_CustomStatesAndTransitions(t *testing.T) {
	t.Parallel()

	t.Run("Simple machine definition with an inline map", func(t *testing.T) {
		machine, err := NewSimple("online", map[string][]string{
			"online":  {"offline", "error"},
			"offline": {"online", "error"},
			"error":   {},
		})
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())

		err = machine.Transition("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
	})

	t.Run("Advanced transition definition by creating a transitions object", func(t *testing.T) {
		customTransitions := transitions.MustNew(map[string][]string{
			"online":  {"offline", "error"},
			"offline": {"online", "error"},
			"error":   {},
		})
		machine, err := New("online", customTransitions)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())

		err = machine.Transition("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
	})
}

// TestReadme_FSMCreationExamples tests FSM creation examples from README.md
func TestReadme_FSMCreationExamples(t *testing.T) {
	t.Parallel()

	t.Run("Creating an FSM using the Typical Transition Set", func(t *testing.T) {
		machine, err := New(
			transitions.StatusNew,
			transitions.Typical,
			WithLogger(slog.Default()),
		)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, transitions.StatusNew, machine.GetState())
	})
}

// TestReadme_PreTransitionHooks tests the pre-transition hook example from README.md
func TestReadme_PreTransitionHooks(t *testing.T) {
	t.Parallel()

	customTransitions := transitions.MustNew(map[string][]string{
		"online":  {"offline"},
		"offline": {"online"},
	})

	logger := slog.Default()
	registry, err := hooks.NewRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(customTransitions),
	)
	require.NoError(t, err)

	connectionEstablished := false
	establishConnection := func() error {
		connectionEstablished = true
		return nil
	}

	err = registry.RegisterPreTransitionHook(hooks.PreTransitionHookConfig{
		Name: "establish-connection",
		From: []string{"offline"},
		To:   []string{"online"},
		Guard: func(ctx context.Context, from, to string) error {
			return establishConnection()
		},
	})
	require.NoError(t, err)

	machine, err := New("offline", customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition("online")
	require.NoError(t, err)
	assert.Equal(t, "online", machine.GetState())
	assert.True(t, connectionEstablished)
}

// TestReadme_PostTransitionHooks tests the post-transition hook example from README.md
func TestReadme_PostTransitionHooks(t *testing.T) {
	t.Parallel()

	customTransitions := transitions.MustNew(map[string][]string{
		"online":  {"offline"},
		"offline": {"online"},
	})

	logger := slog.Default()
	registry, err := hooks.NewRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(customTransitions),
	)
	require.NoError(t, err)

	var recordedTransitions []string
	recordTransition := func(from, to string) {
		recordedTransitions = append(recordedTransitions, from+"->"+to)
	}

	err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
		Name: "record-transitions",
		From: []string{"*"},
		To:   []string{"*"},
		Action: func(ctx context.Context, from, to string) {
			recordTransition(from, to)
		},
	})
	require.NoError(t, err)

	machine, err := New("offline", customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition("online")
	require.NoError(t, err)

	err = machine.Transition("offline")
	require.NoError(t, err)

	expectedTransitions := []string{
		"offline->online",
		"online->offline",
	}
	assert.Equal(t, expectedTransitions, recordedTransitions)
}

// TestReadme_CombiningCallbacks tests the combining callbacks example from README.md
func TestReadme_CombiningCallbacks(t *testing.T) {
	t.Parallel()

	customTransitions := transitions.MustNew(map[string][]string{
		"online":  {"offline"},
		"offline": {"online"},
	})

	logger := slog.Default()
	registry, err := hooks.NewRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(customTransitions),
	)
	require.NoError(t, err)

	preHookCalled := false
	postHookCalled := false

	err = registry.RegisterPreTransitionHook(hooks.PreTransitionHookConfig{
		Name: "validate-offline-online",
		From: []string{"offline"},
		To:   []string{"online"},
		Guard: func(ctx context.Context, from, to string) error {
			logger.Info("validating transition", "from", from, "to", to)
			preHookCalled = true
			return nil
		},
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
		Name: "notify-state-change",
		From: []string{"*"},
		To:   []string{"*"},
		Action: func(ctx context.Context, from, to string) {
			logger.Info("state changed", "from", from, "to", to)
			postHookCalled = true
		},
	})
	require.NoError(t, err)

	machine, err := New("offline", customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition("online")
	require.NoError(t, err)
	assert.Equal(t, "online", machine.GetState())
	assert.True(t, preHookCalled)
	assert.True(t, postHookCalled)
}

// TestReadme_WildcardPatterns tests the wildcard pattern example from README.md
func TestReadme_WildcardPatterns(t *testing.T) {
	t.Parallel()

	customTransitions := transitions.MustNew(map[string][]string{
		"running": {"stopped", "error"},
		"stopped": {"running", "error"},
		"error":   {},
	})

	logger := slog.Default()
	registry, err := hooks.NewRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(customTransitions),
	)
	require.NoError(t, err)

	var preErrorTransitions []string
	var postRunningTransitions []string
	var allTransitions []string

	err = registry.RegisterPreTransitionHook(hooks.PreTransitionHookConfig{
		Name: "log-error-transitions",
		From: []string{"*"},
		To:   []string{"error"},
		Guard: func(ctx context.Context, from, to string) error {
			preErrorTransitions = append(preErrorTransitions, from)
			return nil
		},
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
		Name: "log-leaving-running",
		From: []string{"running"},
		To:   []string{"*"},
		Action: func(ctx context.Context, from, to string) {
			postRunningTransitions = append(postRunningTransitions, to)
		},
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
		Name: "record-all-transitions",
		From: []string{"*"},
		To:   []string{"*"},
		Action: func(ctx context.Context, from, to string) {
			allTransitions = append(allTransitions, from+"->"+to)
		},
	})
	require.NoError(t, err)

	machine, err := New("running", customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition("error")
	require.NoError(t, err)
	assert.Equal(t, []string{"running"}, preErrorTransitions)
	assert.Equal(t, []string{"error"}, postRunningTransitions)
	assert.Equal(t, []string{"running->error"}, allTransitions)
}

// TestReadme_SubscribingToStateChanges tests the subscribing to state changes example from README.md
func TestReadme_SubscribingToStateChanges(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logger := slog.Default()
		ctx, cancel := context.WithCancel(t.Context()) // Use t.Context() for synctest
		defer cancel()

		// 1. Create a broadcast manager
		manager := broadcast.NewManager(logger.Handler())

		// 2. Register the broadcast hook
		registry, err := hooks.NewRegistry(hooks.WithTransitions(transitions.Typical))
		require.NoError(t, err)
		err = registry.RegisterPostTransitionHook(hooks.PostTransitionHookConfig{
			Name:   "broadcast",
			From:   []string{"*"},
			To:     []string{"*"},
			Action: manager.BroadcastHook,
		})
		require.NoError(t, err)

		// 3. Create the FSM
		machine, err := New(
			transitions.StatusNew,
			transitions.Typical,
			WithCallbackRegistry(registry),
		)
		require.NoError(t, err)

		// 4. Get a channel
		stateChan, err := manager.GetStateChan(ctx, broadcast.WithTimeout(-1))
		require.NoError(t, err)

		// 5. Start a listener
		var receivedStates []string
		go func() {
			for state := range stateChan {
				receivedStates = append(receivedStates, state)
			}
		}()

		// The manager does not broadcast the initial state; you can do it manually
		manager.Broadcast(machine.GetState())
		synctest.Wait() // Wait for the first broadcast to be processed

		// 6. Transitions will now broadcast
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)
		synctest.Wait() // Wait for the second

		err = machine.Transition(transitions.StatusRunning)
		require.NoError(t, err)
		synctest.Wait() // Wait for the third

		expected := []string{transitions.StatusNew, transitions.StatusBooting, transitions.StatusRunning}
		assert.Equal(t, expected, receivedStates)
	})
}

// TestReadme_StateTransitionOperations tests state transition examples from README.md
func TestReadme_StateTransitionOperations(t *testing.T) {
	t.Parallel()

	machine, err := NewSimple("online", map[string][]string{
		"online":  {"offline"},
		"offline": {"online"},
	})
	require.NoError(t, err)

	t.Run("Transition", func(t *testing.T) {
		err := machine.Transition("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
		// transition back for next test
		err = machine.Transition("online")
		require.NoError(t, err)
	})

	t.Run("TransitionWithContext", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err = machine.TransitionWithContext(ctx, "offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
		// transition back for next test
		err = machine.Transition("online")
		require.NoError(t, err)
	})

	t.Run("TransitionIfCurrentState", func(t *testing.T) {
		err = machine.TransitionIfCurrentState("online", "offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())

		err = machine.TransitionIfCurrentState("online", "offline")
		require.ErrorIs(t, err, ErrCurrentStateIncorrect)
		assert.Equal(t, "offline", machine.GetState())
		// transition back for next test
		err = machine.Transition("online")
		require.NoError(t, err)
	})

	t.Run("SetState", func(t *testing.T) {
		err = machine.SetState("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
		// transition back for next test
		err = machine.Transition("online")
		require.NoError(t, err)
	})

	t.Run("GetState", func(t *testing.T) {
		currentState := machine.GetState()
		assert.Equal(t, "online", currentState)
	})
}
