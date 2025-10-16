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

	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a new FSM with initial state and inline transitions
	machine, err := NewSimple("new", map[string][]string{
		"new":      {"booting", "error"},
		"booting":  {"running", "error"},
		"running":  {"stopping", "error"},
		"stopping": {"stopped", "error"},
		"stopped":  {"new", "error"},
		"error":    {},
	}, WithLogger(logger))
	require.NoError(t, err)

	// Perform state transitions - they must follow allowed transitions
	// new -> booting -> running -> stopping -> stopped
	err = machine.Transition("booting")
	require.NoError(t, err)
	assert.Equal(t, "booting", machine.GetState())

	err = machine.Transition("running")
	require.NoError(t, err)
	assert.Equal(t, "running", machine.GetState())

	err = machine.Transition("stopping")
	require.NoError(t, err)
	assert.Equal(t, "stopping", machine.GetState())

	err = machine.Transition("stopped")
	require.NoError(t, err)
	assert.Equal(t, "stopped", machine.GetState())
}

// TestReadme_CustomStatesAndTransitions tests the custom states example from README.md
func TestReadme_CustomStatesAndTransitions(t *testing.T) {
	t.Parallel()

	t.Run("Simple approach with inline map", func(t *testing.T) {
		// Simple approach with inline map
		machine, err := NewSimple("online", map[string][]string{
			"online":  {"offline", "unknown"},
			"offline": {"online", "unknown"},
			"unknown": {},
		})
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())

		// Test basic transition
		err = machine.Transition("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
	})

	t.Run("Advanced approach with reusable transition config", func(t *testing.T) {
		// Advanced approach with reusable transition config
		customTransitions := transitions.MustNew(map[string][]string{
			"online":  {"offline", "unknown"},
			"offline": {"online", "unknown"},
			"unknown": {},
		})
		machine, err := New("online", customTransitions)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())

		// Test basic transition
		err = machine.Transition("offline")
		require.NoError(t, err)
		assert.Equal(t, "offline", machine.GetState())
	})
}

// TestReadme_FSMCreationExamples tests FSM creation examples from README.md
func TestReadme_FSMCreationExamples(t *testing.T) {
	t.Parallel()

	t.Run("Simple constructor with inline transitions", func(t *testing.T) {
		// Simple constructor with inline transitions
		machine, err := NewSimple("online", map[string][]string{
			"online":  {"offline"},
			"offline": {"online"},
		})
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())
	})

	t.Run("Advanced constructor with predefined transitions", func(t *testing.T) {
		// Advanced constructor with predefined transitions
		machine, err := New(transitions.StatusNew, transitions.Typical)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, transitions.StatusNew, machine.GetState())
	})

	t.Run("With custom logger options", func(t *testing.T) {
		// With custom logger options
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		logger := slog.New(handler)
		machine, err := NewSimple("online", map[string][]string{
			"online":  {"offline"},
			"offline": {"online"},
		}, WithLogger(logger))
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, "online", machine.GetState())
	})
}

// TestReadme_PreTransitionHooks tests the pre-transition hook example from README.md
func TestReadme_PreTransitionHooks(t *testing.T) {
	t.Parallel()

	const (
		StatusOnline  = "StatusOnline"
		StatusOffline = "StatusOffline"
		StatusUnknown = "StatusUnknown"
	)

	customTransitions := transitions.MustNew(map[string][]string{
		StatusOnline:  {StatusOffline, StatusUnknown},
		StatusOffline: {StatusOnline, StatusUnknown},
		StatusUnknown: {},
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

	err = registry.RegisterPreTransitionHook([]string{StatusOffline}, []string{StatusOnline}, func(ctx context.Context, from, to string) error {
		return establishConnection()
	})
	require.NoError(t, err)

	machine, err := New(StatusOffline, customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition(StatusOnline)
	require.NoError(t, err)
	assert.Equal(t, StatusOnline, machine.GetState())
	assert.True(t, connectionEstablished)
}

// TestReadme_PostTransitionHooks tests the post-transition hook example from README.md
func TestReadme_PostTransitionHooks(t *testing.T) {
	t.Parallel()

	const (
		StatusOnline  = "StatusOnline"
		StatusOffline = "StatusOffline"
		StatusUnknown = "StatusUnknown"
	)

	customTransitions := transitions.MustNew(map[string][]string{
		StatusOnline:  {StatusOffline, StatusUnknown},
		StatusOffline: {StatusOnline, StatusUnknown},
		StatusUnknown: {},
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

	err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
		recordTransition(from, to)
	})
	require.NoError(t, err)

	machine, err := New(StatusOffline, customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition(StatusOnline)
	require.NoError(t, err)

	err = machine.Transition(StatusOffline)
	require.NoError(t, err)

	expectedTransitions := []string{
		StatusOffline + "->" + StatusOnline,
		StatusOnline + "->" + StatusOffline,
	}
	assert.Equal(t, expectedTransitions, recordedTransitions)
}

// TestReadme_CombiningCallbacks tests the combining callbacks example from README.md
func TestReadme_CombiningCallbacks(t *testing.T) {
	t.Parallel()

	const (
		StatusOnline  = "StatusOnline"
		StatusOffline = "StatusOffline"
		StatusUnknown = "StatusUnknown"
	)

	customTransitions := transitions.MustNew(map[string][]string{
		StatusOnline:  {StatusOffline, StatusUnknown},
		StatusOffline: {StatusOnline, StatusUnknown},
		StatusUnknown: {},
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

	err = registry.RegisterPreTransitionHook([]string{StatusOffline}, []string{StatusOnline}, func(ctx context.Context, from, to string) error {
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

	machine, err := New(StatusOffline, customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition(StatusOnline)
	require.NoError(t, err)
	assert.Equal(t, StatusOnline, machine.GetState())
	assert.True(t, cleanupCalled)
	assert.True(t, connectCalled)
	assert.True(t, notifyCalled)
}

// TestReadme_WildcardPatterns tests the wildcard pattern example from README.md
func TestReadme_WildcardPatterns(t *testing.T) {
	t.Parallel()

	const (
		StatusRunning = "StatusRunning"
		StatusError   = "StatusError"
		StatusStopped = "StatusStopped"
	)

	customTransitions := transitions.MustNew(map[string][]string{
		StatusRunning: {StatusStopped, StatusError},
		StatusStopped: {StatusRunning, StatusError},
		StatusError:   {},
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

	err = registry.RegisterPreTransitionHook([]string{"*"}, []string{StatusError}, func(ctx context.Context, from, to string) error {
		preErrorTransitions = append(preErrorTransitions, from)
		return nil
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook([]string{StatusRunning}, []string{"*"}, func(ctx context.Context, from, to string) {
		postRunningTransitions = append(postRunningTransitions, to)
	})
	require.NoError(t, err)

	err = registry.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, func(ctx context.Context, from, to string) {
		allTransitions = append(allTransitions, from+"->"+to)
	})
	require.NoError(t, err)

	machine, err := New(StatusRunning, customTransitions,
		WithLogger(logger),
		WithCallbackRegistry(registry),
	)
	require.NoError(t, err)

	err = machine.Transition(StatusError)
	require.NoError(t, err)
	assert.Equal(t, []string{StatusRunning}, preErrorTransitions)
	assert.Equal(t, []string{StatusError}, postRunningTransitions)
	assert.Equal(t, []string{StatusRunning + "->" + StatusError}, allTransitions)
}

// TestReadme_StateTransitionOperations tests state transition examples from README.md
func TestReadme_StateTransitionOperations(t *testing.T) {
	t.Parallel()

	const (
		StatusOnline  = "StatusOnline"
		StatusOffline = "StatusOffline"
		StatusUnknown = "StatusUnknown"
	)

	customTransitions := transitions.MustNew(map[string][]string{
		StatusOnline:  {StatusOffline, StatusUnknown},
		StatusOffline: {StatusOnline, StatusUnknown},
		StatusUnknown: {},
	})

	t.Run("Simple transition", func(t *testing.T) {
		machine, err := New(StatusOnline, customTransitions)
		require.NoError(t, err)

		err = machine.Transition(StatusOffline)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, machine.GetState())
	})

	t.Run("Conditional transition", func(t *testing.T) {
		machine, err := New(StatusOnline, customTransitions)
		require.NoError(t, err)

		// Correct current state
		err = machine.TransitionIfCurrentState(StatusOnline, StatusOffline)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, machine.GetState())

		// Incorrect current state
		err = machine.TransitionIfCurrentState(StatusOnline, StatusUnknown)
		require.ErrorIs(t, err, ErrCurrentStateIncorrect)
		assert.Equal(t, StatusOffline, machine.GetState())
	})

	t.Run("Get current state", func(t *testing.T) {
		machine, err := New(StatusOnline, customTransitions)
		require.NoError(t, err)

		currentState := machine.GetState()
		assert.Equal(t, StatusOnline, currentState)

		err = machine.Transition(StatusOffline)
		require.NoError(t, err)

		currentState = machine.GetState()
		assert.Equal(t, StatusOffline, currentState)
	})
}
