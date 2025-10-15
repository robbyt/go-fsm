package fsm

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/robbyt/go-fsm/hooks"
	"github.com/robbyt/go-fsm/hooks/broadcast"
	"github.com/robbyt/go-fsm/transitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadme_QuickStartExample tests the complete Quick Start example from README.md
func TestReadme_QuickStartExample(t *testing.T) {
	t.Parallel()

	// Replicate the exact Quick Start example from README.md

	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a new FSM with initial state and predefined transitions
	machine, err := New(logger.Handler(), transitions.StatusNew, transitions.TypicalTransitions)
	require.NoError(t, err)

	// Create standalone broadcast manager
	broadcastManager := broadcast.NewManager(logger)

	// Manually register broadcast hook
	if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
		err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
		require.NoError(t, err)
	}

	// Subscribe to state changes
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stateChan, err := broadcastManager.GetStateChan(ctx, broadcast.WithBufferSize(10))
	require.NoError(t, err)

	// Send initial state
	broadcastManager.Broadcast(machine.GetState())

	var stateChanges []string
	var wg sync.WaitGroup

	wg.Go(func() {
		for state := range stateChan {
			stateChanges = append(stateChanges, state)
		}
	})

	// Perform state transitions- they must follow allowed transitions
	// booting -> running -> stopping -> stopped
	err = machine.Transition(transitions.StatusBooting)
	require.NoError(t, err)

	err = machine.Transition(transitions.StatusRunning)
	require.NoError(t, err)

	err = machine.Transition(transitions.StatusStopping)
	require.NoError(t, err)

	err = machine.Transition(transitions.StatusStopped)
	require.NoError(t, err)

	// Cancel context to close channels
	cancel()

	// Wait for goroutine to finish processing all states
	wg.Wait()

	// Verify all expected state changes were received
	expectedStates := []string{transitions.StatusNew, transitions.StatusBooting, transitions.StatusRunning, transitions.StatusStopping, transitions.StatusStopped}
	assert.Equal(t, expectedStates, stateChanges)
}

// TestReadme_CustomStatesAndTransitions tests the custom states example from README.md
func TestReadme_CustomStatesAndTransitions(t *testing.T) {
	t.Parallel()

	// Define custom states (from README example)
	const (
		StatusOnline  = "StatusOnline"
		StatusOffline = "StatusOffline"
		StatusUnknown = "StatusUnknown"
	)

	// Define allowed transitions (from README example)
	customTransitions := transitions.MustNew(map[string][]string{
		StatusOnline:  {StatusOffline, StatusUnknown},
		StatusOffline: {StatusOnline, StatusUnknown},
		StatusUnknown: {},
	})

	t.Run("Create FSM with custom transitions", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), StatusOnline, customTransitions)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, StatusOnline, machine.GetState())
	})

	t.Run("Test allowed transitions", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), StatusOnline, customTransitions)
		require.NoError(t, err)

		// Online -> Offline (allowed)
		err = machine.Transition(StatusOffline)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, machine.GetState())

		// Offline -> Online (allowed)
		err = machine.Transition(StatusOnline)
		require.NoError(t, err)
		assert.Equal(t, StatusOnline, machine.GetState())

		// Online -> Unknown (allowed)
		err = machine.Transition(StatusUnknown)
		require.NoError(t, err)
		assert.Equal(t, StatusUnknown, machine.GetState())
	})

	t.Run("Test disallowed transitions from Unknown", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), StatusUnknown, customTransitions)
		require.NoError(t, err)

		// Unknown -> Online (not allowed)
		err = machine.Transition(StatusOnline)
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, StatusUnknown, machine.GetState())

		// Unknown -> Offline (not allowed)
		err = machine.Transition(StatusOffline)
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, StatusUnknown, machine.GetState())
	})
}

// TestReadme_FSMCreationExamples tests FSM creation examples from README.md
func TestReadme_FSMCreationExamples(t *testing.T) {
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

	t.Run("Create with default options", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), StatusOnline, customTransitions)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, StatusOnline, machine.GetState())
	})

	t.Run("Create with custom logger options", func(t *testing.T) {
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		machine, err := New(handler, StatusOnline, customTransitions)
		require.NoError(t, err)
		require.NotNil(t, machine)
		assert.Equal(t, StatusOnline, machine.GetState())
	})
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
		machine, err := New(slog.Default().Handler(), StatusOnline, customTransitions)
		require.NoError(t, err)

		err = machine.Transition(StatusOffline)
		require.NoError(t, err)
		assert.Equal(t, StatusOffline, machine.GetState())
	})

	t.Run("Conditional transition", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), StatusOnline, customTransitions)
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
		machine, err := New(slog.Default().Handler(), StatusOnline, customTransitions)
		require.NoError(t, err)

		currentState := machine.GetState()
		assert.Equal(t, StatusOnline, currentState)

		err = machine.Transition(StatusOffline)
		require.NoError(t, err)

		currentState = machine.GetState()
		assert.Equal(t, StatusOffline, currentState)
	})
}

// TestReadme_StateChangeNotifications tests the state change notification examples from README.md
func TestReadme_StateChangeNotifications(t *testing.T) {
	t.Parallel()

	t.Run("Basic state change notifications example", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		// Create standalone broadcast manager
		broadcastManager := broadcast.NewManager(slog.Default())

		// Manually register broadcast hook
		if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
			err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
			require.NoError(t, err)
		}

		// Get notification channel with default async behavior (timeout=0)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		stateChan, err := broadcastManager.GetStateChan(ctx, broadcast.WithBufferSize(10))
		require.NoError(t, err)

		// Send initial state
		broadcastManager.Broadcast(machine.GetState())

		// Track received states
		var receivedStates []string
		var wg sync.WaitGroup

		// Process state changes (replicating README example)
		wg.Go(func() {
			for state := range stateChan {
				// Handle state change
				receivedStates = append(receivedStates, state)
			}
		})

		// Perform some transitions
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)

		err = machine.Transition(transitions.StatusRunning)
		require.NoError(t, err)

		// Cancel to close channel
		cancel()

		// Wait for goroutine to finish
		wg.Wait()

		// Verify we received all state changes
		expectedStates := []string{transitions.StatusNew, transitions.StatusBooting, transitions.StatusRunning}
		assert.Equal(t, expectedStates, receivedStates)
	})
}

// TestReadme_BroadcastModes tests the broadcast mode examples from README.md
func TestReadme_BroadcastModes(t *testing.T) {
	t.Parallel()

	t.Run("Async mode (timeout=0) - default behavior", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		// Create standalone broadcast manager
		broadcastManager := broadcast.NewManager(slog.Default())

		// Manually register broadcast hook
		if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
			err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
			require.NoError(t, err)
		}

		// Get notification channel with default async behavior (timeout=0)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		stateChan, err := broadcastManager.GetStateChan(ctx, broadcast.WithBufferSize(10))
		require.NoError(t, err)

		// Send initial state
		broadcastManager.Broadcast(machine.GetState())

		var receivedStates []string
		var wg sync.WaitGroup

		// Process state changes
		wg.Go(func() {
			for state := range stateChan {
				receivedStates = append(receivedStates, state)
			}
		})

		// Perform transition
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)

		cancel()
		wg.Wait()

		expectedStates := []string{transitions.StatusNew, transitions.StatusBooting}
		assert.Equal(t, expectedStates, receivedStates)
	})

	t.Run("Use sync broadcast with 10s timeout (WithSyncBroadcast)", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		// Create standalone broadcast manager
		broadcastManager := broadcast.NewManager(slog.Default())

		// Manually register broadcast hook
		if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
			err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
			require.NoError(t, err)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()

		// Use sync broadcast with a 10s timeout (WithSyncBroadcast is a shortcut for settings a 10s timeout)
		syncChan, err := broadcastManager.GetStateChan(ctx, broadcast.WithSyncBroadcast())
		require.NoError(t, err)

		// Send initial state
		broadcastManager.Broadcast(machine.GetState())

		var receivedStates []string
		var wg sync.WaitGroup

		// Read and print all state changes from the channel (replicating README example)
		wg.Go(func() {
			for state := range syncChan {
				// fmt.Println("State:", state) - commented out to avoid test output pollution
				receivedStates = append(receivedStates, state)
			}
		})

		// Perform transition
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)

		cancel()
		wg.Wait()

		expectedStates := []string{transitions.StatusNew, transitions.StatusBooting}
		assert.Equal(t, expectedStates, receivedStates)
	})

	t.Run("Use sync broadcast with 1hr custom timeout", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		// Create standalone broadcast manager
		broadcastManager := broadcast.NewManager(slog.Default())

		// Manually register broadcast hook
		if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
			err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
			require.NoError(t, err)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()

		// Use sync broadcast with 1hr custom timeout
		timeoutChan, err := broadcastManager.GetStateChan(ctx, broadcast.WithSyncTimeout(1*time.Hour))
		require.NoError(t, err)

		// Send initial state
		broadcastManager.Broadcast(machine.GetState())

		var receivedStates []string
		var wg sync.WaitGroup

		wg.Go(func() {
			for state := range timeoutChan {
				receivedStates = append(receivedStates, state)
			}
		})

		// Perform transition
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)

		cancel()
		wg.Wait()

		expectedStates := []string{transitions.StatusNew, transitions.StatusBooting}
		assert.Equal(t, expectedStates, receivedStates)
	})

	t.Run("Use infinite blocking (never times out)", func(t *testing.T) {
		machine, err := New(slog.Default().Handler(), transitions.StatusNew, transitions.TypicalTransitions)
		require.NoError(t, err)

		// Create standalone broadcast manager
		broadcastManager := broadcast.NewManager(slog.Default())

		// Manually register broadcast hook
		if reg, ok := machine.callbacks.(*hooks.SynchronousCallbackRegistry); ok {
			err = reg.RegisterPostTransitionHook([]string{"*"}, []string{"*"}, broadcastManager.BroadcastHook())
			require.NoError(t, err)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()

		// Use infinite blocking (never times out)
		infiniteChan, err := broadcastManager.GetStateChan(ctx, broadcast.WithSyncTimeout(-1))
		require.NoError(t, err)

		// Send initial state
		broadcastManager.Broadcast(machine.GetState())

		var receivedStates []string
		var wg sync.WaitGroup

		wg.Go(func() {
			for state := range infiniteChan {
				receivedStates = append(receivedStates, state)
			}
		})

		// Perform transition
		err = machine.Transition(transitions.StatusBooting)
		require.NoError(t, err)

		cancel()
		wg.Wait()

		expectedStates := []string{transitions.StatusNew, transitions.StatusBooting}
		assert.Equal(t, expectedStates, receivedStates)
	})
}
