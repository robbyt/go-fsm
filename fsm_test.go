package fsm

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransitionDB implements the transitionDB interface for unit testing
type mockTransitionDB struct {
	allowedTransitions map[string][]string
	states             []string
}

func newMockTransitionDB(transitions map[string][]string) *mockTransitionDB {
	states := make([]string, 0, len(transitions))
	for state := range transitions {
		states = append(states, state)
	}
	return &mockTransitionDB{
		allowedTransitions: transitions,
		states:             states,
	}
}

func (m *mockTransitionDB) IsTransitionAllowed(from, to string) bool {
	allowed, ok := m.allowedTransitions[from]
	if !ok {
		return false
	}
	for _, state := range allowed {
		if state == to {
			return true
		}
	}
	return false
}

func (m *mockTransitionDB) HasState(state string) bool {
	_, ok := m.allowedTransitions[state]
	return ok
}

func (m *mockTransitionDB) GetAllStates() []string {
	return append([]string(nil), m.states...)
}

// mockCallbackExecutor implements the CallbackExecutor interface for unit testing
type mockCallbackExecutor struct {
	mu                sync.Mutex
	preTransitionErr  error
	preTransitions    []transition
	postTransitions   []transition
	receivedContexts  []context.Context
	shouldPanicOnPre  bool
	shouldPanicOnPost bool
}

type transition struct {
	from string
	to   string
}

func newMockCallbackExecutor() *mockCallbackExecutor {
	return &mockCallbackExecutor{
		preTransitions:   []transition{},
		postTransitions:  []transition{},
		receivedContexts: []context.Context{},
	}
}

func (m *mockCallbackExecutor) ExecutePreTransitionHooks(ctx context.Context, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shouldPanicOnPre {
		panic("pre-transition panic")
	}
	m.receivedContexts = append(m.receivedContexts, ctx)
	m.preTransitions = append(m.preTransitions, transition{from, to})
	return m.preTransitionErr
}

func (m *mockCallbackExecutor) ExecutePostTransitionHooks(ctx context.Context, from, to string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shouldPanicOnPost {
		panic("post-transition panic")
	}
	m.receivedContexts = append(m.receivedContexts, ctx)
	m.postTransitions = append(m.postTransitions, transition{from, to})
}

func (m *mockCallbackExecutor) getPreTransitions() []transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]transition(nil), m.preTransitions...)
}

func (m *mockCallbackExecutor) getPostTransitions() []transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]transition(nil), m.postTransitions...)
}

func (m *mockCallbackExecutor) getReceivedContexts() []context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]context.Context(nil), m.receivedContexts...)
}

// Unit tests with mocked dependencies
func TestFSM_New_Validation(t *testing.T) {
	t.Parallel()

	t.Run("Nil transitions returns error", func(t *testing.T) {
		fsm, err := New("initial", nil)
		assert.Nil(t, fsm)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidConfiguration)
	})

	t.Run("Initial state not in transitions returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		fsm, err := New("invalid", mockDB)
		assert.Nil(t, fsm)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidConfiguration)
	})

	t.Run("Valid initialization succeeds", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		fsm, err := New("state1", mockDB)
		require.NoError(t, err)
		require.NotNil(t, fsm)
		assert.Equal(t, "state1", fsm.GetState())
	})
}

func TestFSM_GetState(t *testing.T) {
	t.Parallel()

	mockDB := newMockTransitionDB(map[string][]string{
		"initial": {"next"},
		"next":    {},
	})

	fsm, err := New("initial", mockDB)
	require.NoError(t, err)

	assert.Equal(t, "initial", fsm.GetState())
}

func TestFSM_SetState(t *testing.T) {
	t.Parallel()

	t.Run("SetState to valid state succeeds", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state3"},
			"state3": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.SetState("state2")
		require.NoError(t, err)
		assert.Equal(t, "state2", fsm.GetState())
	})

	t.Run("SetState to invalid state returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.SetState("invalid")
		require.ErrorIs(t, err, ErrInvalidConfiguration)
		assert.Equal(t, "state1", fsm.GetState()) // State should not change
	})

	t.Run("SetState calls post-transition hooks", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.SetState("state2")
		require.NoError(t, err)

		postTransitions := mockCallbacks.getPostTransitions()
		require.Len(t, postTransitions, 1)
		assert.Equal(t, "state1", postTransitions[0].from)
		assert.Equal(t, "state2", postTransitions[0].to)
	})
}

func TestFSM_Transition(t *testing.T) {
	t.Parallel()

	t.Run("Valid transition succeeds", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state3"},
			"state3": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.NoError(t, err)
		assert.Equal(t, "state2", fsm.GetState())
	})

	t.Run("Invalid transition returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		// state1 -> state3 is not allowed
		err = fsm.Transition("state3")
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, "state1", fsm.GetState())
	})

	t.Run("Transition to non-existent state returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.Transition("nonexistent")
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, "state1", fsm.GetState())
	})
}

func TestFSM_Transition_WithCallbacks(t *testing.T) {
	t.Parallel()

	t.Run("Pre and post-transition hooks execute in order", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.NoError(t, err)

		preTransitions := mockCallbacks.getPreTransitions()
		postTransitions := mockCallbacks.getPostTransitions()

		require.Len(t, preTransitions, 1)
		require.Len(t, postTransitions, 1)

		assert.Equal(t, "state1", preTransitions[0].from)
		assert.Equal(t, "state2", preTransitions[0].to)
		assert.Equal(t, "state1", postTransitions[0].from)
		assert.Equal(t, "state2", postTransitions[0].to)
	})

	t.Run("Pre-transition hook error prevents transition", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()
		mockCallbacks.preTransitionErr = assert.AnError

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.Error(t, err)
		assert.Equal(t, "state1", fsm.GetState()) // State should not change

		preTransitions := mockCallbacks.getPreTransitions()
		postTransitions := mockCallbacks.getPostTransitions()

		require.Len(t, preTransitions, 1) // Pre-hook was called
		require.Empty(t, postTransitions) // Post-hook was NOT called
	})
}

func TestFSM_TransitionBool(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		currentState   string
		targetState    string
		allowed        bool
		expectedResult bool
		expectedState  string
	}{
		{
			name:           "Valid transition returns true",
			currentState:   "state1",
			targetState:    "state2",
			allowed:        true,
			expectedResult: true,
			expectedState:  "state2",
		},
		{
			name:           "Invalid transition returns false",
			currentState:   "state1",
			targetState:    "state3",
			allowed:        false,
			expectedResult: false,
			expectedState:  "state1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transitions := map[string][]string{
				"state1": {},
				"state2": {},
				"state3": {},
			}
			if tc.allowed {
				transitions[tc.currentState] = []string{tc.targetState}
			}

			mockDB := newMockTransitionDB(transitions)
			fsm, err := New(tc.currentState, mockDB)
			require.NoError(t, err)

			result := fsm.TransitionBool(tc.targetState)
			assert.Equal(t, tc.expectedResult, result)
			assert.Equal(t, tc.expectedState, fsm.GetState())
		})
	}
}

func TestFSM_TransitionIfCurrentState(t *testing.T) {
	t.Parallel()

	t.Run("Transition succeeds when current state matches", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.TransitionIfCurrentState("state1", "state2")
		require.NoError(t, err)
		assert.Equal(t, "state2", fsm.GetState())
	})

	t.Run("Transition fails when current state does not match", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state3"},
			"state3": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		err = fsm.TransitionIfCurrentState("state2", "state3")
		require.ErrorIs(t, err, ErrCurrentStateIncorrect)
		assert.Equal(t, "state1", fsm.GetState()) // State should not change
	})

	t.Run("Transition fails when transition is invalid", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		// state1 is correct, but transition to state3 is not allowed
		err = fsm.TransitionIfCurrentState("state1", "state3")
		require.ErrorIs(t, err, ErrInvalidStateTransition)
		assert.Equal(t, "state1", fsm.GetState())
	})
}

func TestFSM_JSONPersistence(t *testing.T) {
	t.Parallel()

	testHandler := slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{Level: slog.LevelError},
	)

	mockDB := newMockTransitionDB(map[string][]string{
		"running":   {"stopped"},
		"stopped":   {},
		"reloading": {"running"},
	})

	fsm, err := New("running", mockDB, WithLogHandler(testHandler))
	require.NoError(t, err)

	jsonData, err := json.Marshal(fsm)
	require.NoError(t, err)

	expectedJSON := `{"state":"running"}`
	assert.JSONEq(t, expectedJSON, string(jsonData))
}

func TestFSM_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("Concurrent reads are safe", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = fsm.GetState()
			}()
		}
		wg.Wait()
	})

	t.Run("Concurrent writes are safe", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {"state1"},
		})

		fsm, err := New("state1", mockDB)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Error intentionally ignored - testing concurrent safety not success
				//nolint:errcheck
				_ = fsm.Transition("state2")
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Error intentionally ignored - testing concurrent safety not success
				//nolint:errcheck
				_ = fsm.Transition("state1")
			}()
		}
		wg.Wait()

		// Final state should be either state1 or state2
		finalState := fsm.GetState()
		assert.Contains(t, []string{"state1", "state2"}, finalState)
	})
}

func TestFSM_Options(t *testing.T) {
	t.Parallel()

	t.Run("WithLogger option sets custom logger", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		fsm, err := New("state1", mockDB, WithLogger(logger))
		require.NoError(t, err)
		require.NotNil(t, fsm)
		assert.NotNil(t, fsm.logger)
	})

	t.Run("WithLogHandler option creates logger from handler", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		handler := slog.NewTextHandler(os.Stdout, nil)
		fsm, err := New("state1", mockDB, WithLogHandler(handler))
		require.NoError(t, err)
		require.NotNil(t, fsm)
		assert.NotNil(t, fsm.logger)
	})

	t.Run("WithCallbackRegistry option sets callback executor", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)
		require.NotNil(t, fsm)

		// Verify callbacks are executed
		err = fsm.Transition("state2")
		require.NoError(t, err)

		postTransitions := mockCallbacks.getPostTransitions()
		require.Len(t, postTransitions, 1)
	})

	t.Run("Nil logger in WithLogger returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		fsm, err := New("state1", mockDB, WithLogger(nil))
		require.Error(t, err)
		assert.Nil(t, fsm)
	})

	t.Run("Nil handler in WithLogHandler returns error", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {},
		})

		fsm, err := New("state1", mockDB, WithLogHandler(nil))
		require.Error(t, err)
		assert.Nil(t, fsm)
	})
}

// TestFSM_TransitionWithContext verifies context flows through to hooks
func TestFSM_TransitionWithContext(t *testing.T) {
	t.Parallel()

	type contextKey string
	const requestIDKey contextKey = "request_id"

	t.Run("Context with values flows to hooks", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		// Create context with a value
		ctx := context.WithValue(context.Background(), requestIDKey, "req-123")

		err = fsm.TransitionWithContext(ctx, "state2")
		require.NoError(t, err)

		// Verify hooks received the context
		receivedContexts := mockCallbacks.getReceivedContexts()
		require.Len(t, receivedContexts, 2) // pre + post hooks

		// Check pre-transition hook got the context
		preCtx := receivedContexts[0]
		assert.Equal(t, "req-123", preCtx.Value(requestIDKey))

		// Check post-transition hook got the context
		postCtx := receivedContexts[1]
		assert.Equal(t, "req-123", postCtx.Value(requestIDKey))
	})

	t.Run("Regular Transition uses Background context", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		err = fsm.Transition("state2")
		require.NoError(t, err)

		// Verify hooks received context (should be Background)
		receivedContexts := mockCallbacks.getReceivedContexts()
		require.Len(t, receivedContexts, 2)

		// Background context has no values
		preCtx := receivedContexts[0]
		assert.Nil(t, preCtx.Value(requestIDKey))
	})

	t.Run("TransitionIfCurrentStateWithContext flows context", func(t *testing.T) {
		mockDB := newMockTransitionDB(map[string][]string{
			"state1": {"state2"},
			"state2": {},
		})
		mockCallbacks := newMockCallbackExecutor()

		fsm, err := New("state1", mockDB, WithCallbackRegistry(mockCallbacks))
		require.NoError(t, err)

		ctx := context.WithValue(context.Background(), requestIDKey, "req-456")

		err = fsm.TransitionIfCurrentStateWithContext(ctx, "state1", "state2")
		require.NoError(t, err)

		// Verify context flowed through
		receivedContexts := mockCallbacks.getReceivedContexts()
		require.Len(t, receivedContexts, 2)
		assert.Equal(t, "req-456", receivedContexts[0].Value(requestIDKey))
		assert.Equal(t, "req-456", receivedContexts[1].Value(requestIDKey))
	})
}
