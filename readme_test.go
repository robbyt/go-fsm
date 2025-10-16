package fsm

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/robbyt/go-fsm/v2/hooks"
	"github.com/robbyt/go-fsm/v2/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadme_QuickStartExample tests the complete Quick Start example from README.md
func TestReadme_QuickStartExample(t *testing.T) {
	t.Parallel()

	logger := slog.Default()

	// Create a new FSM with initial state and inline transitions
	machine, err := NewSimple("new", map[string][]string{
		"new":      {"booting", "error"},
		"booting":  {"running", "error"},
		"running":  {"stopping", "stopped", "error"},
		"stopping": {"stopped", "error"},
		"stopped":  {"new", "error"},
		"error":    {},
	}, WithLogger(logger))
	require.NoError(t, err)

	// Perform state transitions following the allowed transitions defined above
	// new (initial state) -> booting -> running -> stopping -> stopped
	states := []string{"booting", "running", "stopping", "stopped"}
	for _, state := range states {
		err := machine.Transition(state)
		require.NoError(t, err)
		assert.Equal(t, state, machine.GetState())
	}

	// Invalid transition, state does not exist
	err = machine.Transition("nope")
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

	t.Run("Advanced constructor with predefined transitions", func(t *testing.T) {
		machine, err := New(transitions.StatusNew, transitions.Typical)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, transitions.StatusNew, machine.GetState())
	})

	t.Run("With custom logger options", func(t *testing.T) {
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		machine, err := NewSimple("online", map[string][]string{
			"online":  {"offline"},
			"offline": {"online"},
		}, WithLogHandler(handler))
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())
	})
}

// TestReadme_PreTransitionHooks tests the pre-transition hook example from README.md
func TestReadme_PreTransitionHooks(t *testing.T) {
	t.Parallel()

	customTransitions := transitions.MustNew(map[string][]string{
		"online":  {"offline", "error"},
		"offline": {"online", "error"},
		"error":   {},
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

	err = registry.RegisterPreTransitionHook([]string{"offline"}, []string{"online"}, func(ctx context.Context, from, to string) error {
		return establishConnection()
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
		"online":  {"offline", "error"},
		"offline": {"online", "error"},
		"error":   {},
	})

	logger := slog.Default()
	registry, err := hooks.NewRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(customTransitions),
	)
	require.NoError(t, err)

	var recordedTransitions []string
	err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
		recordedTransitions = append(recordedTransitions, from+"->"+to)
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
		"online":  {"offline", "error"},
		"offline": {"online", "error"},
		"error":   {},
	})

	logger := slog.Default()
	registry, err := hooks.NewRegistry(
		hooks.WithLogger(logger),
		hooks.WithTransitions(customTransitions),
	)
	require.NoError(t, err)

	authorized := true
	cleanupCalled := false
	connectCalled := false
	notifyCalled := false

	isAuthorized := func() bool { return authorized }
	cleanup := func() error {
		cleanupCalled = true
		return nil
	}
	connect := func() error {
		connectCalled = true
		return nil
	}
	notifyStateChange := func(_, _ string) {
		notifyCalled = true
	}

	err = registry.RegisterPreTransitionHook([]string{"offline"}, []string{"online"}, func(ctx context.Context, from, to string) error {
		if !isAuthorized() {
			return errors.New("not authorized")
		}
		if err := cleanup(); err != nil {
			return err
		}
		return connect()
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
		notifyStateChange(from, to)
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
	assert.True(t, cleanupCalled)
	assert.True(t, connectCalled)
	assert.True(t, notifyCalled)
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

	err = registry.RegisterPreTransitionHook([]string{"*"}, []string{"error"}, func(ctx context.Context, from, to string) error {
		preErrorTransitions = append(preErrorTransitions, from)
		return nil
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook([]string{"running"}, []string{"*"}, func(ctx context.Context, from, to string) {
		postRunningTransitions = append(postRunningTransitions, to)
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
		allTransitions = append(allTransitions, from+"->"+to)
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

// TestReadme_StateTransitionOperations tests state transition examples from README.md
func TestReadme_StateTransitionOperations(t *testing.T) {
	t.Parallel()

	customTransitions := transitions.MustNew(map[string][]string{
		"online":  {"offline", "error"},
		"offline": {"online", "error"},
		"error":   {},
	})

	t.Run("Simple transition", func(t *testing.T) {
		machine, err := New("online", customTransitions)
		require.NoError(t, err)

		err = machine.Transition("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
	})

	t.Run("Conditional transition", func(t *testing.T) {
		machine, err := New("online", customTransitions)
		require.NoError(t, err)

		err = machine.TransitionIfCurrentState("online", "offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())

		err = machine.TransitionIfCurrentState("online", "error")
		require.ErrorIs(t, err, ErrCurrentStateIncorrect)
		assert.Equal(t, "offline", machine.GetState())
	})

	t.Run("Get current state", func(t *testing.T) {
		machine, err := New("online", customTransitions)
		require.NoError(t, err)

		currentState := machine.GetState()
		assert.Equal(t, "online", currentState)

		err = machine.Transition("offline")
		require.NoError(t, err)

		currentState = machine.GetState()
		assert.Equal(t, "offline", currentState)
	})
}
